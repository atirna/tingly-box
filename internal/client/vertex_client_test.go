package client

import (
	"context"
	"strings"
	"testing"

	"golang.org/x/oauth2"

	"github.com/tingly-dev/tingly-box/ai"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// Structurally valid service-account JSON. google.CredentialsFromJSON only
// parses the document (the private key is used lazily at token mint), so this
// is enough to exercise parsing and caching without any network I/O.
const fakeSAJSON = `{
  "type": "service_account",
  "project_id": "proj",
  "private_key_id": "kid",
  "private_key": "-----BEGIN PRIVATE KEY-----\nZmFrZQ==\n-----END PRIVATE KEY-----\n",
  "client_email": "sa@proj.iam.gserviceaccount.com",
  "token_uri": "https://oauth2.googleapis.com/token"
}`

func TestValidateCloudBundle_MissingBundle(t *testing.T) {
	err := validateCloudBundle(&typ.Provider{Name: "v", AuthType: typ.AuthTypeGCPVertex})
	if err == nil {
		t.Error("expected error for missing credential bundle")
	}
}

func TestVertexAnthropicOptions_Validation(t *testing.T) {
	// missing required fields → schema validation error
	if _, err := vertexAnthropicOptions(context.Background(), cloudProvider("vertex", typ.AuthTypeGCPVertex, map[string]string{
		ai.CredFieldGCPProjectID: "proj",
	}), "m", typ.SessionID{}); err == nil {
		t.Error("expected error for incomplete GCP bundle")
	}

	// complete bundle but malformed SA JSON → error (caught by schema
	// validation), not a panic
	_, err := vertexAnthropicOptions(context.Background(), cloudProvider("vertex", typ.AuthTypeGCPVertex, map[string]string{
		ai.CredFieldGCPServiceAccountJSON: "{not json",
		ai.CredFieldGCPProjectID:          "proj",
		ai.CredFieldGCPLocation:           "us-east5",
	}), "m", typ.SessionID{})
	if err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Errorf("expected invalid-SA-JSON error, got %v", err)
	}

	// valid bundle → adapter option plus our HTTP-client override, in that
	// order (the override must come after so it wins over the adapter's
	// google-built client and provider proxy semantics are preserved)
	opts, err := vertexAnthropicOptions(context.Background(), cloudProvider("vertex", typ.AuthTypeGCPVertex, map[string]string{
		ai.CredFieldGCPServiceAccountJSON: fakeSAJSON,
		ai.CredFieldGCPProjectID:          "proj",
		ai.CredFieldGCPLocation:           "us-east5",
	}), "m", typ.SessionID{})
	if err != nil {
		t.Fatalf("unexpected error for valid bundle: %v", err)
	}
	if len(opts) != 2 {
		t.Errorf("len(opts) = %d, want 2 (adapter + HTTP-client override)", len(opts))
	}
}

// TestVertexAuthedHTTPClient pins the proxy fix: the client that overrides the
// vertex adapter's google-built one must chain the SA OAuth transport over our
// standard provider transport (proxy_url / UA / logging), not replace it.
func TestVertexAuthedHTTPClient(t *testing.T) {
	provider := cloudProvider("vertex", typ.AuthTypeGCPVertex, map[string]string{
		ai.CredFieldGCPServiceAccountJSON: fakeSAJSON,
		ai.CredFieldGCPProjectID:          "proj",
		ai.CredFieldGCPLocation:           "us-east5",
	})
	creds, err := cachedGoogleCredentials(context.Background(), fakeSAJSON)
	if err != nil {
		t.Fatalf("credentials: %v", err)
	}
	c := vertexAuthedHTTPClient(provider, "m", typ.SessionID{}, creds)
	tr, ok := c.Transport.(*oauth2.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *oauth2.Transport", c.Transport)
	}
	if tr.Source == nil {
		t.Error("oauth2.Transport.Source must carry the SA token source")
	}
	if tr.Base == nil {
		t.Error("oauth2.Transport.Base must be the provider transport chain, not nil (nil would fall back to http.DefaultTransport and drop proxy_url)")
	}
}

// TestCachedGoogleCredentials_Reuse guards the per-request rebuild cost: pool.go
// reconstructs clients per request, so the parsed credentials (and their token
// source) must be reused for an unchanged SA JSON and re-derived when it changes.
func TestCachedGoogleCredentials_Reuse(t *testing.T) {
	ctx := context.Background()
	first, err := cachedGoogleCredentials(ctx, fakeSAJSON)
	if err != nil {
		t.Fatalf("first parse: %v", err)
	}
	second, err := cachedGoogleCredentials(ctx, fakeSAJSON)
	if err != nil {
		t.Fatalf("second parse: %v", err)
	}
	if first != second {
		t.Error("expected cache hit to return the same *google.Credentials")
	}

	changed, err := cachedGoogleCredentials(ctx, strings.Replace(fakeSAJSON, `"proj"`, `"proj2"`, 1))
	if err != nil {
		t.Fatalf("changed parse: %v", err)
	}
	if changed == first {
		t.Error("expected a different SA JSON to produce a new cache entry")
	}
}
