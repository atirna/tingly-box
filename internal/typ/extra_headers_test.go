package typ

import (
	"strings"
	"testing"
)

func TestValidateExtraHeaders(t *testing.T) {
	valid := map[string]string{
		"HTTP-Referer": "https://example.com",
		"X-Title":      "tingly-box",
	}
	if err := ValidateExtraHeaders(valid); err != nil {
		t.Fatalf("valid headers rejected: %v", err)
	}
	if err := ValidateExtraHeaders(nil); err != nil {
		t.Fatalf("nil headers rejected: %v", err)
	}

	rejected := []struct {
		name    string
		headers map[string]string
	}{
		{"empty name", map[string]string{"": "v"}},
		{"invalid name chars", map[string]string{"X Title": "v"}},
		{"invalid value", map[string]string{"X-A": "bad\nvalue"}},
		{"denied Authorization", map[string]string{"Authorization": "Bearer x"}},
		{"denied authorization (case)", map[string]string{"authorization": "Bearer x"}},
		{"denied X-Api-Key", map[string]string{"x-api-key": "sk-1"}},
		{"denied User-Agent", map[string]string{"User-Agent": "me/1.0"}},
		{"denied Host", map[string]string{"Host": "evil.example"}},
		{"denied Content-Length", map[string]string{"Content-Length": "0"}},
		{"case-insensitive duplicate", map[string]string{"X-Dup": "1", "x-dup": "2"}},
		{"name too long", map[string]string{strings.Repeat("A", MaxExtraHeaderNameLen+1): "v"}},
		{"value too long", map[string]string{"X-A": strings.Repeat("v", MaxExtraHeaderValueLen+1)}},
	}
	for _, tc := range rejected {
		if err := ValidateExtraHeaders(tc.headers); err == nil {
			t.Errorf("%s: expected rejection, got nil", tc.name)
		}
	}

	tooMany := map[string]string{}
	for i := 0; i <= MaxExtraHeadersPerLevel; i++ {
		tooMany["X-H-"+strings.Repeat("a", i+1)] = "v"
	}
	if err := ValidateExtraHeaders(tooMany); err == nil {
		t.Error("count over limit: expected rejection, got nil")
	}
}

func TestIsDeniedExtraHeader(t *testing.T) {
	for _, name := range []string{"Authorization", "authorization", "x-api-key", "user-agent", "HOST"} {
		if !IsDeniedExtraHeader(name) {
			t.Errorf("IsDeniedExtraHeader(%q) = false, want true", name)
		}
	}
	if IsDeniedExtraHeader("X-Title") {
		t.Error("X-Title should not be denied")
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
