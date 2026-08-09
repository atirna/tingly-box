package core

import "testing"

// TestBuildAuthConfigPerPlatform checks the auth-map → AuthConfig wiring for
// every platform that declares an Auth mapping, plus the default fallback for
// an unknown platform.
func TestBuildAuthConfigPerPlatform(t *testing.T) {
	tests := []struct {
		platform string
		auth     map[string]string
		wantType string
		check    func(*testing.T, AuthConfig)
	}{
		{
			platform: "telegram",
			auth:     map[string]string{"token": "t"},
			wantType: "token",
			check: func(t *testing.T, c AuthConfig) {
				if c.Token != "t" {
					t.Errorf("Token = %q", c.Token)
				}
			},
		},
		{
			platform: "feishu",
			auth:     map[string]string{"clientId": "id", "clientSecret": "sec"},
			wantType: "oauth",
			check: func(t *testing.T, c AuthConfig) {
				if c.ClientID != "id" || c.ClientSecret != "sec" {
					t.Errorf("oauth fields not mapped: %+v", c)
				}
			},
		},
		{
			platform: "whatsapp",
			auth:     map[string]string{"token": "t", "phoneNumberId": "p"},
			wantType: "token",
			check: func(t *testing.T, c AuthConfig) {
				if c.AccountID != "p" {
					t.Errorf("AccountID = %q, want the phone number id", c.AccountID)
				}
			},
		},
		{
			platform: "weixin",
			auth:     map[string]string{"token": "t", "bot_id": "b", "user_id": "u", "base_url": "https://x"},
			wantType: "qr",
			check: func(t *testing.T, c AuthConfig) {
				if c.AccountID != "b" {
					t.Errorf("AccountID = %q, want bot_id", c.AccountID)
				}
				if c.AuthDir != "u" {
					t.Errorf("AuthDir = %q, want user_id — Weixin reuses this field", c.AuthDir)
				}
			},
		},
		{
			platform: "tingly",
			auth:     map[string]string{},
			wantType: "none",
			check:    func(t *testing.T, c AuthConfig) {},
		},
		{
			// A platform with no table entry falls back to a bot token, which
			// is the least surprising guess for a new IM platform.
			platform: "some-future-platform",
			auth:     map[string]string{"token": "t"},
			wantType: "token",
			check: func(t *testing.T, c AuthConfig) {
				if c.Token != "t" {
					t.Errorf("Token = %q", c.Token)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			cfg := BuildAuthConfig(tt.platform, tt.auth)
			if cfg.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", cfg.Type, tt.wantType)
			}
			tt.check(t, cfg)
			if missing := MissingAuthKeys(tt.platform, tt.auth); len(missing) != 0 {
				t.Errorf("unexpected missing keys: %v", missing)
			}
		})
	}
}

// TestMissingAuthKeysNamesThem covers the operator-facing half: a bot that
// cannot start should say which credential it lacks.
func TestMissingAuthKeysNamesThem(t *testing.T) {
	missing := MissingAuthKeys("feishu", map[string]string{"clientId": "id"})
	if len(missing) != 1 || missing[0] != "clientSecret" {
		t.Errorf("missing = %v, want [clientSecret]", missing)
	}

	if missing := MissingAuthKeys("tingly", nil); len(missing) != 0 {
		t.Errorf("tingly needs no credentials, got %v", missing)
	}
}

// TestDescriptorAuthTypeAgreesWithMapping guards the table against the exact
// drift that broke Lark: a descriptor whose AuthType says one thing but whose
// Auth mapping says another.
func TestDescriptorAuthTypeAgreesWithMapping(t *testing.T) {
	for _, d := range platformDescriptors {
		if d.Auth == nil {
			if d.AuthType != "" {
				t.Errorf("platform %q has AuthType %q but no Auth mapping", d.ID, d.AuthType)
			}
			continue
		}
		if d.Auth.Type != d.AuthType {
			t.Errorf("platform %q: Auth.Type %q disagrees with AuthType %q",
				d.ID, d.Auth.Type, d.AuthType)
		}
	}
}
