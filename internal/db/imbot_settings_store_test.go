package db

import (
	"testing"
	"time"

	"github.com/tingly-dev/tingly-box/internal/constant"
)

// legacyImBotSettingsRecord is ImBotSettingsRecord as it was before
// auth_config and bash_allowlist moved to `serializer:json`: both were
// `string` columns holding hand-marshalled JSON, with "" meaning absent.
type legacyImBotSettingsRecord struct {
	BotUUID       string    `gorm:"primaryKey;column:bot_uuid"`
	Name          string    `gorm:"column:name"`
	Platform      string    `gorm:"column:platform;index:idx_platform"`
	AuthType      string    `gorm:"column:auth_type"`
	AuthConfig    string    `gorm:"column:auth_config;type:text"`
	ProxyURL      string    `gorm:"column:proxy_url"`
	BashAllowlist string    `gorm:"column:bash_allowlist;type:text"`
	DefaultCwd    string    `gorm:"column:default_cwd"`
	Enabled       bool      `gorm:"column:enabled;index:idx_enabled"`
	Debug         bool      `gorm:"column:debug;default:false"`
	Verbose       *bool     `gorm:"column:verbose;default:true"`
	Scenarios     string    `gorm:"column:scenarios;type:text"`
	CreatedAt     time.Time `gorm:"column:created_at"`
	UpdatedAt     time.Time `gorm:"column:updated_at"`
}

func (legacyImBotSettingsRecord) TableName() string { return "imbot_settings" }

func TestImBotSettingsStore_ReadsLegacyJSONStringRows(t *testing.T) {
	dir := t.TempDir()

	legacyDB, err := OpenSQLite(constant.GetDBFile(dir), 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := legacyDB.AutoMigrate(&legacyImBotSettingsRecord{}); err != nil {
		t.Fatalf("legacy migrate: %v", err)
	}
	rows := []legacyImBotSettingsRecord{
		{
			BotUUID: "full", Name: "full", Platform: "telegram", AuthType: "token",
			AuthConfig:    `{"token":"legacy-token","secret":"s3cr3t"}`,
			BashAllowlist: `["ls","cat"]`,
			Enabled:       true,
		},
		{
			// Both JSON columns in the legacy "absent" encoding.
			BotUUID: "bare", Name: "bare", Platform: "weixin",
			AuthConfig: "", BashAllowlist: "", Enabled: false,
		},
	}
	for i := range rows {
		if err := legacyDB.Create(&rows[i]).Error; err != nil {
			t.Fatalf("seed %s: %v", rows[i].BotUUID, err)
		}
	}
	sqlDB, err := legacyDB.DB()
	if err != nil {
		t.Fatalf("legacyDB.DB(): %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	store, err := NewImBotSettingsStore(dir)
	if err != nil {
		t.Fatalf("NewImBotSettingsStore over legacy db: %v", err)
	}
	defer store.Close()

	t.Run("decodes legacy auth and allowlist", func(t *testing.T) {
		got, err := store.GetSettingsByUUID("full")
		if err != nil {
			t.Fatalf("GetSettingsByUUID: %v", err)
		}
		if got.Auth["token"] != "legacy-token" || got.Auth["secret"] != "s3cr3t" {
			t.Errorf("Auth = %v, want token/secret", got.Auth)
		}
		// Token is mirrored out of Auth for backward compatibility.
		if got.Token != "legacy-token" {
			t.Errorf("Token = %q, want legacy-token", got.Token)
		}
		if len(got.BashAllowlist) != 2 || got.BashAllowlist[0] != "ls" {
			t.Errorf("BashAllowlist = %v, want [ls cat]", got.BashAllowlist)
		}
	})

	t.Run("empty legacy columns read as absent, Auth stays non-nil", func(t *testing.T) {
		got, err := store.GetSettingsByUUID("bare")
		if err != nil {
			t.Fatalf("GetSettingsByUUID: %v", err)
		}
		if got.Auth == nil {
			t.Fatal("Auth = nil; callers index and assign into it")
		}
		if len(got.Auth) != 0 {
			t.Errorf("Auth = %v, want empty", got.Auth)
		}
		if len(got.BashAllowlist) != 0 {
			t.Errorf("BashAllowlist = %v, want empty", got.BashAllowlist)
		}
	})

	t.Run("ListSettings decodes every row", func(t *testing.T) {
		all, err := store.ListSettings()
		if err != nil {
			t.Fatalf("ListSettings: %v", err)
		}
		if len(all) != 2 {
			t.Fatalf("ListSettings returned %d, want 2", len(all))
		}
		for _, s := range all {
			if s.Auth == nil {
				t.Errorf("%s: Auth = nil", s.UUID)
			}
		}
	})
}

func TestImBotSettingsStore_RoundTrip(t *testing.T) {
	store, err := NewImBotSettingsStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewImBotSettingsStore: %v", err)
	}
	defer store.Close()

	created, err := store.CreateSettings(Settings{
		Name: "rt", Platform: "telegram", AuthType: "token",
		Auth:          map[string]string{"token": "abc"},
		BashAllowlist: []string{"ls"},
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("CreateSettings: %v", err)
	}

	got, err := store.GetSettingsByUUID(created.UUID)
	if err != nil {
		t.Fatalf("GetSettingsByUUID: %v", err)
	}
	if got.Auth["token"] != "abc" {
		t.Errorf("Auth = %v, want token=abc", got.Auth)
	}
	if len(got.BashAllowlist) != 1 || got.BashAllowlist[0] != "ls" {
		t.Errorf("BashAllowlist = %v, want [ls]", got.BashAllowlist)
	}

	t.Run("absent values store as NULL, not an empty json document", func(t *testing.T) {
		bare, err := store.CreateSettings(Settings{Name: "bare", Platform: "weixin"})
		if err != nil {
			t.Fatalf("CreateSettings: %v", err)
		}
		type row struct {
			AuthConfig    *string
			BashAllowlist *string
		}
		var r row
		if err := store.db.Raw(
			"SELECT auth_config, bash_allowlist FROM imbot_settings WHERE bot_uuid = ?",
			bare.UUID).Scan(&r).Error; err != nil {
			t.Fatalf("raw scan: %v", err)
		}
		if r.AuthConfig != nil {
			t.Errorf("auth_config = %q, want NULL", *r.AuthConfig)
		}
		if r.BashAllowlist != nil {
			t.Errorf("bash_allowlist = %q, want NULL", *r.BashAllowlist)
		}
	})
}

// TestImBotSettingsStore_UpdateIsPartial pins the rule UpdateSettings has
// always documented -- an empty/zero field leaves the stored value alone --
// across the move from Updates(map) to read-modify-write.
func TestImBotSettingsStore_UpdateIsPartial(t *testing.T) {
	store, err := NewImBotSettingsStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewImBotSettingsStore: %v", err)
	}
	defer store.Close()

	created, err := store.CreateSettings(Settings{
		Name: "orig", Platform: "telegram", AuthType: "token",
		Auth:               map[string]string{"token": "orig-token"},
		BashAllowlist:      []string{"ls", "cat"},
		ProxyURL:           "http://proxy:8080",
		DefaultCwd:         "/orig",
		DefaultAgent:       "agent-1",
		SmartGuideProvider: "prov-1",
		SmartGuideModel:    "model-1",
		Scenarios:          `[{"scenario":"a"}]`,
		Enabled:            true,
	})
	if err != nil {
		t.Fatalf("CreateSettings: %v", err)
	}

	// Update only the name; everything else is zero and must survive.
	if err := store.UpdateSettings(created.UUID, Settings{Name: "renamed", Enabled: true}); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}

	got, err := store.GetSettingsByUUID(created.UUID)
	if err != nil {
		t.Fatalf("GetSettingsByUUID: %v", err)
	}
	if got.Name != "renamed" {
		t.Errorf("Name = %q, want renamed", got.Name)
	}
	if got.Auth["token"] != "orig-token" {
		t.Errorf("Auth = %v, want the original token preserved", got.Auth)
	}
	if len(got.BashAllowlist) != 2 {
		t.Errorf("BashAllowlist = %v, want the original 2 entries preserved", got.BashAllowlist)
	}
	if got.ProxyURL != "http://proxy:8080" {
		t.Errorf("ProxyURL = %q, want preserved", got.ProxyURL)
	}
	if got.DefaultCwd != "/orig" || got.DefaultAgent != "agent-1" {
		t.Errorf("defaults not preserved: cwd=%q agent=%q", got.DefaultCwd, got.DefaultAgent)
	}
	if got.SmartGuideProvider != "prov-1" || got.SmartGuideModel != "model-1" {
		t.Errorf("smartguide not preserved: %q/%q", got.SmartGuideProvider, got.SmartGuideModel)
	}
	if got.Platform != "telegram" || got.AuthType != "token" {
		t.Errorf("platform/authtype not preserved: %q/%q", got.Platform, got.AuthType)
	}

	t.Run("scenarios can still be cleared explicitly", func(t *testing.T) {
		if err := store.UpdateSettings(created.UUID, Settings{Enabled: true}); err != nil {
			t.Fatalf("UpdateSettings: %v", err)
		}
		got, err := store.GetSettingsByUUID(created.UUID)
		if err != nil {
			t.Fatalf("GetSettingsByUUID: %v", err)
		}
		if got.Scenarios != "" {
			t.Errorf("Scenarios = %q, want cleared", got.Scenarios)
		}
	})

	t.Run("replacing auth and allowlist works", func(t *testing.T) {
		if err := store.UpdateSettings(created.UUID, Settings{
			Auth:          map[string]string{"token": "new-token"},
			BashAllowlist: []string{"echo"},
			Enabled:       true,
		}); err != nil {
			t.Fatalf("UpdateSettings: %v", err)
		}
		got, err := store.GetSettingsByUUID(created.UUID)
		if err != nil {
			t.Fatalf("GetSettingsByUUID: %v", err)
		}
		if got.Auth["token"] != "new-token" {
			t.Errorf("Auth = %v, want new-token", got.Auth)
		}
		if len(got.BashAllowlist) != 1 || got.BashAllowlist[0] != "echo" {
			t.Errorf("BashAllowlist = %v, want [echo]", got.BashAllowlist)
		}
	})

	t.Run("columns Settings does not model survive an update", func(t *testing.T) {
		// debug/verbose exist on the record but not on Settings. The old
		// Updates(map) path never mentioned them; read-modify-write must not
		// reset them either.
		if err := store.db.Exec(
			"UPDATE imbot_settings SET debug = 1, verbose = 0 WHERE bot_uuid = ?",
			created.UUID).Error; err != nil {
			t.Fatalf("seed debug/verbose: %v", err)
		}
		if err := store.UpdateSettings(created.UUID, Settings{Name: "again", Enabled: true}); err != nil {
			t.Fatalf("UpdateSettings: %v", err)
		}
		var rec ImBotSettingsRecord
		if err := store.db.Where("bot_uuid = ?", created.UUID).First(&rec).Error; err != nil {
			t.Fatalf("reload: %v", err)
		}
		if !rec.Debug {
			t.Error("debug was reset by UpdateSettings, want preserved")
		}
		if rec.Verbose == nil || *rec.Verbose {
			t.Errorf("verbose = %v, want preserved false", rec.Verbose)
		}
	})

	t.Run("unknown uuid is an error", func(t *testing.T) {
		if err := store.UpdateSettings("nope", Settings{Name: "x"}); err == nil {
			t.Error("UpdateSettings on a missing row returned nil, want an error")
		}
	})
}
