package fetcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tingly-dev/tingly-box/ai"
	"github.com/tingly-dev/tingly-box/ai/quota"
)

func opencodeProvider(base string) *ai.Provider {
	return &ai.Provider{UUID: "u-opencode", Name: "OpenCode", Token: "oc-key", APIBase: base}
}

func TestOpenCodeFetcher_Fetch(t *testing.T) {
	var gotPath, gotAuth string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"usage":{
		 "rolling":{"status":"ok","percent":12,"resetsAt":"2026-08-19T13:00:00.000Z"},
		 "weekly":{"status":"ok","percent":40,"resetsAt":"2026-08-24T00:00:00.000Z"},
		 "monthly":{"status":"ok","percent":31,"resetsAt":"2026-09-03T00:00:00.000Z"}}}`))
	}))
	defer s.Close()

	// The provider is configured with the inference base; the usage endpoint
	// hangs off the host root, so the "/zen/v1" prefix has to come off first.
	u, err := (&OpenCodeFetcher{}).Fetch(context.Background(), opencodeProvider(s.URL+"/zen/v1"))
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/zen/go/v1/usage" {
		t.Errorf("path = %q; want /zen/go/v1/usage", gotPath)
	}
	if gotAuth != "Bearer oc-key" {
		t.Errorf("authorization = %q; want the Zen API key as a bearer token", gotAuth)
	}

	// The week is the binding limit even though the 5h window resets sooner:
	// the gateway refuses on whichever is spent first.
	check(t, "opencode", u, want{pct: 40, ok: true, tightest: "weekly", windows: 3})

	rolling := findWindow(t, u, "rolling")
	if rolling.WindowMinutes != 5*60 {
		t.Errorf("rolling = %d min; want 300, the window the gateway names", rolling.WindowMinutes)
	}
	if rolling.Unit != quota.UsageUnitPercent {
		t.Errorf("rolling unit = %q; upstream reports percentages only", rolling.Unit)
	}
	if rolling.ResetsAt == nil || !rolling.ResetsAt.Equal(time.Date(2026, 8, 19, 13, 0, 0, 0, time.UTC)) {
		t.Errorf("rolling ResetsAt = %v; want the upstream timestamp", rolling.ResetsAt)
	}
	if u.RecoversAt() == nil {
		t.Error("a Go plan limit refills on its own; RecoversAt should say when")
	}
}

// A spent window is reported as rate-limited, and that verdict has to survive
// into the window — a floored 100% alone cannot tell "just barely spent" from
// "requests are being refused".
func TestOpenCodeFetcher_RateLimited(t *testing.T) {
	s := serve(t, `{"usage":{
	 "rolling":{"status":"rate-limited","percent":100,"resetsAt":"2026-08-19T15:00:00.000Z"},
	 "weekly":{"status":"ok","percent":70,"resetsAt":"2026-08-24T00:00:00.000Z"},
	 "monthly":{"status":"ok","percent":55,"resetsAt":"2026-09-03T00:00:00.000Z"}}}`)

	u, err := (&OpenCodeFetcher{}).Fetch(context.Background(), opencodeProvider(s.URL))
	if err != nil {
		t.Fatal(err)
	}
	check(t, "opencode limited", u, want{pct: 100, ok: true, tightest: "rolling", windows: 3})

	rolling := findWindow(t, u, "rolling")
	if rolling.LimitReached == nil || !*rolling.LimitReached {
		t.Error("rolling: LimitReached should be true when upstream says rate-limited")
	}
	if rolling.Allowed == nil || *rolling.Allowed {
		t.Error("rolling: Allowed should be false when upstream says rate-limited")
	}
}

// A pay-as-you-go Zen key is a working key with no Go plan behind it. Its
// balance has no public endpoint, so the honest answer is "unknown" — not an
// error the user can act on, and emphatically not 0% used.
func TestOpenCodeFetcher_NoSubscription(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"EntitlementError",
		 "message":"OpenCode Go subscription required."}}`))
	}))
	defer s.Close()

	u, err := (&OpenCodeFetcher{}).Fetch(context.Background(), opencodeProvider(s.URL))
	if err != nil {
		t.Fatalf("a balance-only key is not a fetch failure: %v", err)
	}
	check(t, "opencode balance-only", u, want{ok: false, windows: 0})
	if u.LastError == "" {
		t.Error("the reason the quota is unreadable should be recorded")
	}
}

// A rejected key is a real failure, and the upstream message is what tells the
// user whether the key is wrong or revoked.
func TestOpenCodeFetcher_Unauthorized(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"AuthError","message":"Unauthorized"}}`))
	}))
	defer s.Close()

	if _, err := (&OpenCodeFetcher{}).Fetch(context.Background(), opencodeProvider(s.URL)); err == nil {
		t.Fatal("expected an error for a rejected key")
	}
}

func TestOpenCodeFetcher_Validate(t *testing.T) {
	f := NewOpenCodeFetcher()
	if err := f.Validate(nil); err == nil {
		t.Error("nil provider should not validate")
	}
	if err := f.Validate(&ai.Provider{UUID: "u", Name: "OpenCode"}); err == nil {
		t.Error("a provider with no API key should not validate")
	}
	if err := f.Validate(opencodeProvider("")); err != nil {
		t.Errorf("a key-bearing provider should validate: %v", err)
	}
}
