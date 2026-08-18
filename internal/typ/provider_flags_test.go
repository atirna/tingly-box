package typ

import "testing"

func TestSupplyExtraHeaders(t *testing.T) {
	p := &Provider{
		Flags: ProviderFlags{ExtraHeaders: map[string]string{
			"X-Shared":   "provider",
			"X-Provider": "p",
		}},
		ModelFlags: map[string]ProviderFlags{"m1": {ExtraHeaders: map[string]string{
			"X-Shared": "model",
			"X-Model":  "m",
		}}},
	}

	// provider ∪ model, model wins per name. (The rule level is layered on
	// top by the outbound transport's write order, not merged here.)
	got := SupplyExtraHeaders(p, "m1")
	want := map[string]string{
		"X-Shared":   "model",
		"X-Provider": "p",
		"X-Model":    "m",
	}
	for name, value := range want {
		if got[name] != value {
			t.Errorf("merged[%q] = %q, want %q", name, got[name], value)
		}
	}
	if len(got) != len(want) {
		t.Errorf("merged has %d entries, want %d: %v", len(got), len(want), got)
	}

	// A different model drops the model level, keeping the provider level.
	got = SupplyExtraHeaders(p, "other")
	if got["X-Shared"] != "provider" || got["X-Provider"] != "p" || got["X-Model"] != "" {
		t.Errorf("model-mismatch merge wrong: %v", got)
	}

	// Names are canonicalized so differently-cased levels collide predictably.
	p2 := &Provider{
		Flags:      ProviderFlags{ExtraHeaders: map[string]string{"x-title": "provider"}},
		ModelFlags: map[string]ProviderFlags{"m1": {ExtraHeaders: map[string]string{"X-TITLE": "model"}}},
	}
	got = SupplyExtraHeaders(p2, "m1")
	if len(got) != 1 || got["X-Title"] != "model" {
		t.Errorf("case-insensitive collision not resolved to the model level: %v", got)
	}

	// Nothing configured → nil, so callers can skip injection.
	if got := SupplyExtraHeaders(&Provider{}, "m"); got != nil {
		t.Errorf("expected nil for unconfigured provider, got %v", got)
	}
	if got := SupplyExtraHeaders(nil, "m"); got != nil {
		t.Errorf("expected nil for nil provider, got %v", got)
	}
}

func TestPruneModelFlags(t *testing.T) {
	got := PruneModelFlags(map[string]ProviderFlags{
		"gpt-x":  {ExtraHeaders: map[string]string{"X-Canary": "on"}},
		"":       {ExtraHeaders: map[string]string{"X-Skip": "empty model key"}},
		"zeroed": {},
	})
	if len(got) != 1 || got["gpt-x"].ExtraHeaders["X-Canary"] != "on" {
		t.Errorf("empty-key and zero-value entries should be pruned, got %v", got)
	}

	// An all-pruned map collapses to nil so storage keeps no empty object.
	if got := PruneModelFlags(map[string]ProviderFlags{"m": {}}); got != nil {
		t.Errorf("all-zero map should prune to nil, got %v", got)
	}
	if got := PruneModelFlags(nil); got != nil {
		t.Errorf("nil should prune to nil, got %v", got)
	}
}
