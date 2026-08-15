package db

import (
	"path/filepath"
	"testing"

	"github.com/tingly-dev/tingly-box/internal/constant"
	"github.com/tingly-dev/tingly-box/internal/protocol"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// legacyProviderRecord is the ProviderRecord as it was before the JSON
// columns moved to `serializer:json`: the four JSON payloads were `string`
// fields, marshalled by hand, with "" meaning absent. Rows in every existing
// tingly.db were written through this shape, so the new code has to keep
// reading them.
type legacyProviderRecord struct {
	UUID             string `gorm:"primaryKey;column:uuid"`
	Name             string `gorm:"column:name;not null;index"`
	APIBase          string `gorm:"column:api_base;not null"`
	APIStyle         string `gorm:"column:api_style;not null"`
	AuthType         string `gorm:"column:auth_type;not null"`
	Enabled          bool   `gorm:"column:enabled;default:true"`
	Token            string `gorm:"column:token"`
	Tags             string `gorm:"column:tags;type:text"`
	OAuthExtraFields string `gorm:"column:oauth_extra_fields;type:text"`
	VModelDetail     string `gorm:"column:vmodel_detail;type:text"`
	Credential       string `gorm:"column:credential;type:text"`
}

func (legacyProviderRecord) TableName() string { return "providers" }

// TestProviderStore_ReadsLegacyJSONStringRows writes rows exactly as the
// pre-serializer code did -- including "" for "absent", which is not valid
// JSON -- and reads them back through the migrated store.
func TestProviderStore_ReadsLegacyJSONStringRows(t *testing.T) {
	dir := t.TempDir()

	// Write the legacy rows through a legacy-shaped model.
	legacyDB, err := OpenSQLite(constant.GetDBFile(dir), 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := legacyDB.AutoMigrate(&legacyProviderRecord{}); err != nil {
		t.Fatalf("legacy migrate: %v", err)
	}
	legacyRows := []legacyProviderRecord{
		{
			UUID: "absent", Name: "absent", APIBase: "https://a.example.com",
			APIStyle: "openai", AuthType: "api_key", Enabled: true, Token: "tok",
			// Every JSON column empty -- the legacy "absent" encoding.
			Tags: "", OAuthExtraFields: "", VModelDetail: "", Credential: "",
		},
		{
			UUID: "tagged", Name: "tagged", APIBase: "https://b.example.com",
			APIStyle: "openai", AuthType: "api_key", Enabled: true, Token: "tok",
			Tags: `["alpha","beta"]`,
		},
		{
			UUID: "oauthy", Name: "oauthy", APIBase: "https://c.example.com",
			APIStyle: "anthropic", AuthType: "oauth", Enabled: true, Token: "access",
			OAuthExtraFields: `{"id_token":"legacy-id-token"}`,
		},
		{
			UUID: "virtual", Name: "virtual", APIBase: "https://d.example.com",
			APIStyle: "openai", AuthType: "vmodel", Enabled: true,
			VModelDetail: `{"models":["v1","v2"],"latency_profile":"fast"}`,
		},
		{
			UUID: "bundled", Name: "bundled", APIBase: "https://e.example.com",
			APIStyle: "openai", AuthType: "aws_sigv4", Enabled: true,
			Credential: `{"fields":{"access_key_id":"AKIA","region":"us-east-1"}}`,
		},
	}
	for i := range legacyRows {
		if err := legacyDB.Create(&legacyRows[i]).Error; err != nil {
			t.Fatalf("seed legacy row %s: %v", legacyRows[i].UUID, err)
		}
	}
	sqlDB, err := legacyDB.DB()
	if err != nil {
		t.Fatalf("legacyDB.DB(): %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	// Now open the same file through the migrated store.
	store, err := NewProviderStore(dir)
	if err != nil {
		t.Fatalf("NewProviderStore over legacy db: %v", err)
	}
	defer store.Close()

	t.Run("empty string columns read as absent", func(t *testing.T) {
		p, err := store.GetByUUID("absent")
		if err != nil {
			t.Fatalf("GetByUUID: %v", err)
		}
		if len(p.Tags) != 0 {
			t.Errorf("Tags = %v, want empty", p.Tags)
		}
		if p.VModelDetail != nil {
			t.Errorf("VModelDetail = %+v, want nil", p.VModelDetail)
		}
		if p.Credential != nil {
			t.Errorf("Credential = %+v, want nil", p.Credential)
		}
	})

	t.Run("tags", func(t *testing.T) {
		p, err := store.GetByUUID("tagged")
		if err != nil {
			t.Fatalf("GetByUUID: %v", err)
		}
		if len(p.Tags) != 2 || p.Tags[0] != "alpha" || p.Tags[1] != "beta" {
			t.Errorf("Tags = %v, want [alpha beta]", p.Tags)
		}
	})

	t.Run("oauth extra fields", func(t *testing.T) {
		p, err := store.GetByUUID("oauthy")
		if err != nil {
			t.Fatalf("GetByUUID: %v", err)
		}
		if p.OAuthDetail == nil {
			t.Fatal("OAuthDetail = nil")
		}
		if got := p.OAuthDetail.ExtraFields["id_token"]; got != "legacy-id-token" {
			t.Errorf("ExtraFields[id_token] = %v, want legacy-id-token", got)
		}
	})

	t.Run("vmodel detail", func(t *testing.T) {
		p, err := store.GetByUUID("virtual")
		if err != nil {
			t.Fatalf("GetByUUID: %v", err)
		}
		if p.VModelDetail == nil {
			t.Fatal("VModelDetail = nil")
		}
		if len(p.VModelDetail.Models) != 2 || p.VModelDetail.Models[0] != "v1" {
			t.Errorf("Models = %v, want [v1 v2]", p.VModelDetail.Models)
		}
		if p.VModelDetail.LatencyProfile != "fast" {
			t.Errorf("LatencyProfile = %q, want fast", p.VModelDetail.LatencyProfile)
		}
	})

	t.Run("credential bundle", func(t *testing.T) {
		p, err := store.GetByUUID("bundled")
		if err != nil {
			t.Fatalf("GetByUUID: %v", err)
		}
		if p.Credential == nil {
			t.Fatal("Credential = nil")
		}
		if got := p.Credential.Field("access_key_id"); got != "AKIA" {
			t.Errorf("access_key_id = %q, want AKIA", got)
		}
		if got := p.Credential.Field("region"); got != "us-east-1" {
			t.Errorf("region = %q, want us-east-1", got)
		}
	})

	// The migration must not have rewritten the column types out from under
	// the legacy rows -- the file is still the same one AutoMigrate opened.
	t.Run("db file is the same one", func(t *testing.T) {
		if _, err := store.GetByUUID("tagged"); err != nil {
			t.Fatalf("post-migrate read: %v", err)
		}
		want := filepath.Join(dir, "db", "tingly.db")
		if got := constant.GetDBFile(dir); got != want {
			t.Fatalf("test wired to the wrong path: %s", got)
		}
	})
}

// TestProviderStore_CacheIsolation pins the property the string encoding used
// to provide for free: a provider handed out by the store must not share
// mutable state with the cached record. The OAuth callback handler really
// does mutate ExtraFields on a provider it read from the store, so a shared
// map would let an unsaved (or failed) write leak into the cache.
func TestProviderStore_CacheIsolation(t *testing.T) {
	store, _ := setupTestProviderStore(t)
	defer store.Close()

	seed := &typ.Provider{
		UUID:     "isolation",
		Name:     "isolation",
		APIBase:  "https://api.example.com",
		APIStyle: protocol.APIStyleOpenAI,
		AuthType: typ.AuthTypeAPIKey,
		Token:    "tok",
		Enabled:  true,
		Tags:     []string{"keep"},
	}
	if err := store.Save(seed); err != nil {
		t.Fatalf("Save: %v", err)
	}

	t.Run("caller mutating a returned provider cannot reach the cache", func(t *testing.T) {
		got, err := store.GetByUUID("isolation")
		if err != nil {
			t.Fatalf("GetByUUID: %v", err)
		}
		got.Tags[0] = "tampered"

		again, err := store.GetByUUID("isolation")
		if err != nil {
			t.Fatalf("GetByUUID: %v", err)
		}
		if again.Tags[0] != "keep" {
			t.Errorf("cache saw a caller's mutation: Tags[0] = %q, want keep", again.Tags[0])
		}
	})

	t.Run("mutating the saved provider cannot reach the cache", func(t *testing.T) {
		seed.Tags[0] = "tampered-after-save"

		got, err := store.GetByUUID("isolation")
		if err != nil {
			t.Fatalf("GetByUUID: %v", err)
		}
		if got.Tags[0] != "keep" {
			t.Errorf("cache aliases the caller's slice: Tags[0] = %q, want keep", got.Tags[0])
		}
	})

	t.Run("oauth ExtraFields map is not shared", func(t *testing.T) {
		oauth := &typ.Provider{
			UUID:     "isolation-oauth",
			Name:     "isolation-oauth",
			APIBase:  "https://api.example.com",
			APIStyle: protocol.APIStyleOpenAI,
			AuthType: typ.AuthTypeOAuth,
			Enabled:  true,
			OAuthDetail: &typ.OAuthDetail{
				AccessToken: "access",
				ExtraFields: map[string]any{"id_token": "original"},
			},
		}
		if err := store.Save(oauth); err != nil {
			t.Fatalf("Save oauth: %v", err)
		}

		got, err := store.GetByUUID("isolation-oauth")
		if err != nil {
			t.Fatalf("GetByUUID: %v", err)
		}
		// Exactly what internal/server/module/oauth/handler.go does.
		got.OAuthDetail.ExtraFields["id_token"] = "rotated-but-not-saved"

		again, err := store.GetByUUID("isolation-oauth")
		if err != nil {
			t.Fatalf("GetByUUID: %v", err)
		}
		if v := again.OAuthDetail.ExtraFields["id_token"]; v != "original" {
			t.Errorf("cache saw an unsaved ExtraFields mutation: id_token = %v, want original", v)
		}
	})

	t.Run("credential bundle Fields map is not shared", func(t *testing.T) {
		bundle := &typ.Provider{
			UUID:     "isolation-cred",
			Name:     "isolation-cred",
			APIBase:  "https://api.example.com",
			APIStyle: protocol.APIStyleOpenAI,
			AuthType: typ.AuthTypeAWSSigV4,
			Enabled:  true,
			Credential: &typ.CredentialBundle{
				Fields: map[string]string{"region": "us-east-1"},
			},
		}
		if err := store.Save(bundle); err != nil {
			t.Fatalf("Save bundle: %v", err)
		}

		got, err := store.GetByUUID("isolation-cred")
		if err != nil {
			t.Fatalf("GetByUUID: %v", err)
		}
		got.Credential.Fields["region"] = "eu-west-1"

		again, err := store.GetByUUID("isolation-cred")
		if err != nil {
			t.Fatalf("GetByUUID: %v", err)
		}
		if v := again.Credential.Field("region"); v != "us-east-1" {
			t.Errorf("cache saw an unsaved Credential mutation: region = %q, want us-east-1", v)
		}
	})
}
