package imbot

import (
	"testing"

	"github.com/tingly-dev/tingly-box/imbot/core"
)

// TestPlatformConfigDerivesFromCore asserts the settings-API view takes its
// intrinsic attributes (display name, auth type) from the single source of
// truth in core. After the SSOT consolidation there is no second platform
// table in this package, so this is now a thin guard against regression.
func TestPlatformConfigDerivesFromCore(t *testing.T) {
	for id := range core.PlatformNames {
		cfg, ok := GetPlatformConfig(string(id))
		if !ok {
			t.Errorf("core platform %q not returned by GetPlatformConfig", id)
			continue
		}
		if want := core.GetPlatformName(id); cfg.DisplayName != want {
			t.Errorf("%q: DisplayName = %q, want %q", id, cfg.DisplayName, want)
		}
		if cfg.AuthType != core.GetPlatformAuthType(id) {
			t.Errorf("%q: AuthType not derived from core", id)
		}
		if cfg.Platform != string(id) {
			t.Errorf("%q: Platform = %q, want %q", id, cfg.Platform, id)
		}
	}
}

// TestFormPlatformsAreCreatable is the key anti-drift guarantee carried over
// from the dual-table era: every platform that has a settings/auth form must be
// instantiable by the registry. (The reverse is not required — e.g. Tingly and
// Weixin have registry creators but no traditional form, since their
// credentials come from other flows.)
func TestFormPlatformsAreCreatable(t *testing.T) {
	creatable := make(map[string]bool)
	for _, p := range Platforms() {
		creatable[p] = true
	}
	for id := range platformFormFields {
		if !creatable[id] {
			t.Errorf("platform %q has a config form but no registry creator", id)
		}
	}
}

// TestGetAllPlatformsPopulated ensures the accessor used by the settings API
// returns one fully-populated entry per core platform.
func TestGetAllPlatformsPopulated(t *testing.T) {
	all := GetAllPlatforms()
	if len(all) != len(core.PlatformNames) {
		t.Fatalf("GetAllPlatforms returned %d, want %d (one per core platform)", len(all), len(core.PlatformNames))
	}
	for _, cfg := range all {
		if cfg.DisplayName == "" {
			t.Errorf("GetAllPlatforms returned %q with empty DisplayName", cfg.Platform)
		}
	}
}
