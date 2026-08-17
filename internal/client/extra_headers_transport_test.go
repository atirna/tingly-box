package client

import (
	"net/http"
	"testing"

	"github.com/tingly-dev/tingly-box/ai"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// pinTransport simulates a vendor round-tripper: it force-sets a header on
// its way to the wire (inner chains run after the extra-headers layer).
type pinTransport struct {
	inner http.RoundTripper
	name  string
	value string
}

func (p *pinTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set(p.name, p.value)
	return p.inner.RoundTrip(req)
}

func apiKeyProvider() *typ.Provider {
	return &typ.Provider{UUID: "p1", AuthType: ai.AuthTypeAPIKey, Token: "sk-x"}
}

func newHeadersReq(t *testing.T, headers map[string]string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "https://upstream.example/v1/chat", nil)
	if err != nil {
		t.Fatal(err)
	}
	if headers != nil {
		req = req.WithContext(typ.WithExtraHeaders(req.Context(), headers))
	}
	return req
}

func TestExtraHeadersTransport_AppliesCtxHeaders(t *testing.T) {
	capture := &captureTransport{}
	rt := wrapWithExtraHeaders(capture, apiKeyProvider())

	if _, err := rt.RoundTrip(newHeadersReq(t, map[string]string{"X-Title": "tingly"})); err != nil {
		t.Fatal(err)
	}
	if got := capture.lastReq.Header.Get("X-Title"); got != "tingly" {
		t.Errorf("X-Title = %q, want tingly", got)
	}
}

func TestExtraHeadersTransport_NonAPIKeyIsNoOp(t *testing.T) {
	capture := &captureTransport{}
	oauth := &typ.Provider{UUID: "p2", AuthType: ai.AuthTypeOAuth}

	rt := wrapWithExtraHeaders(capture, oauth)
	if rt != http.RoundTripper(capture) {
		t.Fatal("non-api_key provider must get the inner transport unchanged")
	}

	// Even ctx-carried rule headers cannot reach a non-api_key chain.
	if _, err := rt.RoundTrip(newHeadersReq(t, map[string]string{"X-Rule": "r"})); err != nil {
		t.Fatal(err)
	}
	if got := capture.lastReq.Header.Get("X-Rule"); got != "" {
		t.Errorf("X-Rule = %q, want unset on non-api_key provider", got)
	}
}

// TestExtraHeadersTransport_VendorPinWins is the ordering invariant of
// .design/provider-flags.md §5.2: the extra-headers layer sits outside vendor
// round-trippers, which write later (closer to the wire) and therefore win on
// a name conflict. Moot for the api_key-only release, but it must hold from
// day one for a future OAuth rollout.
func TestExtraHeadersTransport_VendorPinWins(t *testing.T) {
	capture := &captureTransport{}
	vendor := &pinTransport{inner: capture, name: "X-Vendor-Pin", value: "pinned"}
	rt := wrapWithExtraHeaders(vendor, apiKeyProvider())

	if _, err := rt.RoundTrip(newHeadersReq(t, map[string]string{
		"X-Vendor-Pin": "user-tries-to-override",
		"X-Title":      "tingly",
	})); err != nil {
		t.Fatal(err)
	}
	if got := capture.lastReq.Header.Get("X-Vendor-Pin"); got != "pinned" {
		t.Errorf("X-Vendor-Pin = %q, want pinned (vendor writes last)", got)
	}
	if got := capture.lastReq.Header.Get("X-Title"); got != "tingly" {
		t.Errorf("X-Title = %q, want tingly (non-conflicting header still applied)", got)
	}
}

// TestExtraHeadersTransport_DenylistDefense: config normally cannot contain
// denied names (ValidateExtraHeaders rejects on save), but imports or old
// rows might — the transport must skip them rather than touch
// gateway-managed headers.
func TestExtraHeadersTransport_DenylistDefense(t *testing.T) {
	capture := &captureTransport{}
	rt := wrapWithExtraHeaders(capture, apiKeyProvider())

	req := newHeadersReq(t, map[string]string{
		"Authorization": "Bearer attacker",
		"X-Title":       "fine",
	})
	req.Header.Set("Authorization", "Bearer real")
	if _, err := rt.RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	if got := capture.lastReq.Header.Get("Authorization"); got != "Bearer real" {
		t.Errorf("Authorization = %q, want the untouched original", got)
	}
	if got := capture.lastReq.Header.Get("X-Title"); got != "fine" {
		t.Errorf("X-Title = %q, want fine", got)
	}
}

func TestExtraHeadersTransport_DoesNotMutateCallerRequest(t *testing.T) {
	capture := &captureTransport{}
	rt := wrapWithExtraHeaders(capture, apiKeyProvider())

	orig := newHeadersReq(t, map[string]string{"X-Title": "tingly"})
	if _, err := rt.RoundTrip(orig); err != nil {
		t.Fatal(err)
	}
	if got := orig.Header.Get("X-Title"); got != "" {
		t.Errorf("caller's request mutated: X-Title = %q", got)
	}
}

// TestWrapWithLogging_MountsExtraHeadersLayer pins the wiring: the single
// wrapWithLogging choke point mounts the extra-headers layer for api_key
// providers, so every client constructor picks it up without per-constructor
// code.
func TestWrapWithLogging_MountsExtraHeadersLayer(t *testing.T) {
	capture := &captureTransport{}
	rt := wrapWithLogging(capture, apiKeyProvider())

	if _, err := rt.RoundTrip(newHeadersReq(t, map[string]string{"X-Title": "tingly"})); err != nil {
		t.Fatal(err)
	}
	if got := capture.lastReq.Header.Get("X-Title"); got != "tingly" {
		t.Errorf("X-Title = %q, want tingly (headers layer not mounted)", got)
	}
}
