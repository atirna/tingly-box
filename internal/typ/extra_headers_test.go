package typ

import (
	"testing"
)

func TestValidateExtraHeaders(t *testing.T) {
	// User-driven: any structurally valid header is accepted — including
	// gateway-adjacent names like Authorization or User-Agent. The user owns
	// the outcome; precedence with managed headers is decided by transport
	// ordering, not filtering.
	accepted := []map[string]string{
		nil,
		{"HTTP-Referer": "https://example.com", "X-Title": "tingly-box"},
		{"Authorization": "Bearer custom"},
		{"User-Agent": "me/1.0"},
		{"X-Api-Key": "k"},
		{"X-Empty-Value": ""},
	}
	for i, headers := range accepted {
		if err := ValidateExtraHeaders(headers); err != nil {
			t.Errorf("accepted[%d] rejected: %v", i, err)
		}
	}

	// Structural rejections only: malformed names/values would fail the HTTP
	// request itself; case-insensitive duplicates have no defined winner.
	rejected := []struct {
		name    string
		headers map[string]string
	}{
		{"empty name", map[string]string{"": "v"}},
		{"invalid name chars", map[string]string{"X Title": "v"}},
		{"invalid value", map[string]string{"X-A": "bad\nvalue"}},
		{"case-insensitive duplicate", map[string]string{"X-Dup": "1", "x-dup": "2"}},
	}
	for _, tc := range rejected {
		if err := ValidateExtraHeaders(tc.headers); err == nil {
			t.Errorf("%s: expected rejection, got nil", tc.name)
		}
	}
}

func TestCanonicalizeExtraHeaders(t *testing.T) {
	out := CanonicalizeExtraHeaders(map[string]string{
		"http-referer": "https://example.com",
		"X-TITLE":      "t",
	})
	if _, ok := out["Http-Referer"]; !ok {
		t.Errorf("http-referer not canonicalized: %v", out)
	}
	if _, ok := out["X-Title"]; !ok {
		t.Errorf("X-TITLE not canonicalized: %v", out)
	}
	if got := CanonicalizeExtraHeaders(nil); got != nil {
		t.Errorf("nil should canonicalize to nil, got %v", got)
	}
}
