package imbot

import "testing"

// TestGetAllPlatformsDerivesFromCore guards the SSOT move: every platform
// GetAllPlatforms returns must carry its display name from core.PlatformDescriptor,
// and every platform an operator can configure (those with a settings form)
// must carry a non-empty auth type from core too. Planned-only platforms
// (googlechat/signal/bluebubbles) legitimately have no auth type yet.
func TestGetAllPlatformsDerivesFromCore(t *testing.T) {
	all := GetAllPlatforms()
	if len(all) == 0 {
		t.Fatal("GetAllPlatforms returned no platforms")
	}
	for _, cfg := range all {
		if cfg.DisplayName == "" {
			t.Errorf("platform %q has empty DisplayName — should come from core", cfg.Platform)
		}
		if _, hasForm := platformFormFields[cfg.Platform]; hasForm && cfg.AuthType == "" {
			t.Errorf("platform %q has a settings form but empty AuthType — should come from core", cfg.Platform)
		}
	}
}

// TestGetPlatformConfigUnknown ensures unknown platforms report not-ok rather
// than returning a zero-value entry the frontend would render as a real platform.
func TestGetPlatformConfigUnknown(t *testing.T) {
	if _, ok := GetPlatformConfig("not-a-real-platform"); ok {
		t.Error("GetPlatformConfig should report ok=false for unknown platforms")
	}
}

// TestPlatformFormFieldsAgreeWithCoreAuthType guards the UI form map against
// drifting from the runtime table: a platform's form must exist only for a
// platform registered in core, and every form-required field must be among
// core's required auth keys (otherwise the UI lets an operator save a bot that
// cannot start). This is the Lark-bug guard relocated to the SSOT world.
func TestPlatformFormFieldsAgreeWithCoreAuthType(t *testing.T) {
	for id, fields := range platformFormFields {
		cfg, ok := GetPlatformConfig(id)
		if !ok {
			t.Errorf("platform %q has a form but no core descriptor", id)
			continue
		}
		for _, f := range fields {
			if f.Required && !contains(cfg.Auth.RequiredKeys, f.Key) {
				t.Errorf("platform %q: form marks %q required but core auth mapping does not", id, f.Key)
			}
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// TestBuildAuthConfigLark is the regression test for the defect the table
// replaced: Lark bots were handed a token-type auth config that the Feishu
// client rejects. Kept here at the imbot (re-export) surface because that is
// how the bot manager calls it.
func TestBuildAuthConfigLark(t *testing.T) {
	auth := map[string]string{"clientId": "cli_x", "clientSecret": "sec"}

	cfg := BuildAuthConfig("lark", auth)
	if cfg.Type != "oauth" {
		t.Errorf("Type = %q, want oauth — the Feishu client rejects anything else", cfg.Type)
	}
	if cfg.ClientID != "cli_x" || cfg.ClientSecret != "sec" {
		t.Errorf("credentials not mapped: %+v", cfg)
	}
	if missing := MissingAuthKeys("lark", auth); len(missing) != 0 {
		t.Errorf("a fully configured Lark bot reports missing keys: %v", missing)
	}
}

// TestAuthOptionsWeixin covers credentials that travel as connection options
// rather than as auth fields.
func TestAuthOptionsWeixin(t *testing.T) {
	opts := AuthOptions("weixin", map[string]string{
		"token": "t", "bot_id": "b", "user_id": "u", "base_url": "https://x",
	})
	if opts["user_id"] != "u" || opts["base_url"] != "https://x" {
		t.Errorf("weixin options = %v", opts)
	}
	if _, leaked := opts["token"]; leaked {
		t.Error("token belongs in AuthConfig, not in connection options")
	}

	if opts := AuthOptions("telegram", map[string]string{"token": "t"}); opts != nil {
		t.Errorf("telegram has no option-carried credentials, got %v", opts)
	}
}
