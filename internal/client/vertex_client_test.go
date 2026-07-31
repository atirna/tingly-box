package client

import (
	"context"
	"strings"
	"testing"

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

func TestVertexAnthropicOption_Validation(t *testing.T) {
	// missing required fields → schema validation error
	if _, err := vertexAnthropicOption(context.Background(), cloudProvider("vertex", typ.AuthTypeGCPVertex, map[string]string{
		ai.CredFieldGCPProjectID: "proj",
	})); err == nil {
		t.Error("expected error for incomplete GCP bundle")
	}

	// complete bundle but malformed SA JSON → error (caught by schema
	// validation), not a panic
	_, err := vertexAnthropicOption(context.Background(), cloudProvider("vertex", typ.AuthTypeGCPVertex, map[string]string{
		ai.CredFieldGCPServiceAccountJSON: "{not json",
		ai.CredFieldGCPProjectID:          "proj",
		ai.CredFieldGCPLocation:           "us-east5",
	}))
	if err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Errorf("expected invalid-SA-JSON error, got %v", err)
	}

	// valid bundle → option resolves
	if _, err := vertexAnthropicOption(context.Background(), cloudProvider("vertex", typ.AuthTypeGCPVertex, map[string]string{
		ai.CredFieldGCPServiceAccountJSON: fakeSAJSON,
		ai.CredFieldGCPProjectID:          "proj",
		ai.CredFieldGCPLocation:           "us-east5",
	})); err != nil {
		t.Errorf("unexpected error for valid bundle: %v", err)
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
