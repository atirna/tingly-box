package db

import (
	"testing"
	"time"

	"github.com/tingly-dev/tingly-box/internal/constant"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// legacyProviderModelRecord is ProviderModelRecord as it was before the
// models column moved to `serializer:json`: a `string` holding hand-marshalled
// JSON, with "" meaning "no list ever stored".
type legacyProviderModelRecord struct {
	ProviderUUID string    `gorm:"primaryKey;column:provider_uuid"`
	ProviderName string    `gorm:"column:provider_name;index"`
	APIBase      string    `gorm:"column:api_base"`
	Models       string    `gorm:"column:models;type:text"`
	Source       string    `gorm:"column:source"`
	LastUpdated  time.Time `gorm:"column:last_updated"`
	CreatedAt    time.Time `gorm:"column:created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at"`
}

func (legacyProviderModelRecord) TableName() string { return "provider_models" }

func seedLegacyModelRows(t *testing.T, dir string, rows []legacyProviderModelRecord) {
	t.Helper()
	db, err := OpenSQLite(constant.GetDBFile(dir), 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(&legacyProviderModelRecord{}); err != nil {
		t.Fatalf("legacy migrate: %v", err)
	}
	for i := range rows {
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatalf("seed %s: %v", rows[i].ProviderUUID, err)
		}
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB(): %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// TestModelStore_ReadsLegacyModelRows covers the read paths against rows in
// the pre-serializer encoding.
func TestModelStore_ReadsLegacyModelRows(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	seedLegacyModelRows(t, dir, []legacyProviderModelRecord{
		// A successful fetch with models.
		{ProviderUUID: "has-models", ProviderName: "a", APIBase: "https://a",
			Models: `["gpt-4","gpt-4o"]`, Source: "api", LastUpdated: now},
		// A successful fetch that returned nothing -- stored as "[]".
		{ProviderUUID: "empty-list", ProviderName: "b", APIBase: "https://b",
			Models: `[]`, Source: "api", LastUpdated: now},
		// Never a successful fetch -- the legacy "absent" encoding.
		{ProviderUUID: "never-fetched", ProviderName: "c", APIBase: "https://c",
			Models: ``},
	})

	store, err := NewModelStore(dir)
	if err != nil {
		t.Fatalf("NewModelStore over legacy db: %v", err)
	}
	defer store.Close()

	t.Run("GetModels decodes a legacy list", func(t *testing.T) {
		got := store.GetModels("has-models", 0)
		if len(got) != 2 || got[0] != "gpt-4" || got[1] != "gpt-4o" {
			t.Errorf("GetModels = %v, want [gpt-4 gpt-4o]", got)
		}
	})

	t.Run("GetModels on a never-fetched row is empty", func(t *testing.T) {
		if got := store.GetModels("never-fetched", 0); len(got) != 0 {
			t.Errorf("GetModels = %v, want empty", got)
		}
	})

	t.Run("GetModelCount", func(t *testing.T) {
		if got := store.GetModelCount("has-models"); got != 2 {
			t.Errorf("GetModelCount = %d, want 2", got)
		}
		if got := store.GetModelCount("empty-list"); got != 0 {
			t.Errorf("GetModelCount(empty-list) = %d, want 0", got)
		}
	})

	// The predicate has to keep the legacy "" row out while letting the
	// legacy "[]" and "[...]" rows in -- the behaviour it had pre-migration.
	t.Run("GetAllProviders excludes never-fetched", func(t *testing.T) {
		got := store.GetAllProviders()
		if !contains(got, "has-models") {
			t.Errorf("GetAllProviders = %v, want it to contain has-models", got)
		}
		if !contains(got, "empty-list") {
			t.Errorf("GetAllProviders = %v, want it to contain empty-list (stored []), matching pre-migration behaviour", got)
		}
		if contains(got, "never-fetched") {
			t.Errorf("GetAllProviders = %v, want it to exclude never-fetched (legacy \"\")", got)
		}
	})

	t.Run("HasModels", func(t *testing.T) {
		if !store.HasModels("has-models") {
			t.Error("HasModels(has-models) = false, want true")
		}
		if store.HasModels("never-fetched") {
			t.Error("HasModels(never-fetched) = true, want false")
		}
	})

	t.Run("GetProviderInfo", func(t *testing.T) {
		if _, _, ok := store.GetProviderInfo("has-models"); !ok {
			t.Error("GetProviderInfo(has-models) not found, want found")
		}
		if _, _, ok := store.GetProviderInfo("never-fetched"); ok {
			t.Error("GetProviderInfo(never-fetched) found, want not found")
		}
	})
}

// TestModelStore_NewRowsMatchPredicate is the other half: rows written by the
// migrated store use NULL for "absent", which the same predicate must handle.
func TestModelStore_NewRowsMatchPredicate(t *testing.T) {
	dir := t.TempDir()
	store, err := NewModelStore(dir)
	if err != nil {
		t.Fatalf("NewModelStore: %v", err)
	}
	defer store.Close()

	withModels := &typ.Provider{UUID: "with", Name: "with", APIBase: "https://w"}
	emptyList := &typ.Provider{UUID: "empty", Name: "empty", APIBase: "https://e"}
	failedOnly := &typ.Provider{UUID: "failed", Name: "failed", APIBase: "https://f"}

	if err := store.SaveModels(withModels, []string{"m1", "m2"}, ModelSourceAPI); err != nil {
		t.Fatalf("SaveModels: %v", err)
	}
	if err := store.SaveModels(emptyList, []string{}, ModelSourceAPI); err != nil {
		t.Fatalf("SaveModels empty: %v", err)
	}
	if err := store.SaveFetchFailure(failedOnly, "boom", nil, time.Time{}); err != nil {
		t.Fatalf("SaveFetchFailure: %v", err)
	}

	t.Run("raw column encoding", func(t *testing.T) {
		type row struct {
			ProviderUUID string
			Models       *string
		}
		var rows []row
		if err := store.db.Raw("SELECT provider_uuid, models FROM provider_models ORDER BY provider_uuid").
			Scan(&rows).Error; err != nil {
			t.Fatalf("raw scan: %v", err)
		}
		for _, r := range rows {
			switch r.ProviderUUID {
			case "with":
				if r.Models == nil || *r.Models != `["m1","m2"]` {
					t.Errorf("with: models = %v, want [\"m1\",\"m2\"]", r.Models)
				}
			case "empty":
				if r.Models == nil || *r.Models != `[]` {
					t.Errorf("empty: models = %v, want []", r.Models)
				}
			case "failed":
				if r.Models != nil {
					t.Errorf("failed: models = %q, want NULL", *r.Models)
				}
			}
		}
	})

	t.Run("GetAllProviders", func(t *testing.T) {
		got := store.GetAllProviders()
		if !contains(got, "with") {
			t.Errorf("GetAllProviders = %v, want it to contain with", got)
		}
		if !contains(got, "empty") {
			t.Errorf("GetAllProviders = %v, want it to contain empty (stored [])", got)
		}
		if contains(got, "failed") {
			t.Errorf("GetAllProviders = %v, want it to exclude failed (NULL models)", got)
		}
	})

	t.Run("HasModels", func(t *testing.T) {
		if !store.HasModels("with") {
			t.Error("HasModels(with) = false, want true")
		}
		if store.HasModels("failed") {
			t.Error("HasModels(failed) = true, want false")
		}
	})

	// A failure-only write must not clobber a previously fetched list.
	t.Run("failure-only write preserves the stale list", func(t *testing.T) {
		if err := store.SaveFetchFailure(withModels, "later boom", nil, time.Time{}); err != nil {
			t.Fatalf("SaveFetchFailure: %v", err)
		}
		got := store.GetModels("with", 0)
		if len(got) != 2 || got[0] != "m1" {
			t.Errorf("GetModels after failure = %v, want the stale [m1 m2]", got)
		}
		if lastErr, ok := store.GetFetchFailure("with"); !ok || lastErr != "later boom" {
			t.Errorf("GetFetchFailure = %q,%v, want \"later boom\",true", lastErr, ok)
		}
	})

	// And a later success must clear the error it left behind.
	t.Run("success clears the recorded failure", func(t *testing.T) {
		if err := store.SaveModels(withModels, []string{"m3"}, ModelSourceAPI); err != nil {
			t.Fatalf("SaveModels: %v", err)
		}
		if lastErr, ok := store.GetFetchFailure("with"); ok {
			t.Errorf("GetFetchFailure = %q, want cleared", lastErr)
		}
		if got := store.GetModels("with", 0); len(got) != 1 || got[0] != "m3" {
			t.Errorf("GetModels = %v, want [m3]", got)
		}
	})
}

// TestHasStoredModelListScope pins the scope against every value the column
// can hold under either encoding, in one place.
func TestHasStoredModelListScope(t *testing.T) {
	db, err := OpenSQLite(constant.GetDBFile(t.TempDir()), 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(&ProviderModelRecord{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cases := []struct {
		uuid  string
		value any // nil -> SQL NULL
		want  bool
	}{
		{"legacy-absent", "", false},
		{"legacy-list", `["a"]`, true},
		{"legacy-empty-list", `[]`, true},
		{"new-absent", nil, false},
		{"new-list", `["a"]`, true},
		{"new-empty-list", `[]`, true},
	}
	for _, c := range cases {
		if err := db.Exec("INSERT INTO provider_models (provider_uuid, models) VALUES (?, ?)",
			c.uuid, c.value).Error; err != nil {
			t.Fatalf("insert %s: %v", c.uuid, err)
		}
	}

	var matched []ProviderModelRecord
	if err := db.Scopes(hasStoredModelList).Find(&matched).Error; err != nil {
		t.Fatalf("scoped find: %v", err)
	}
	got := make(map[string]bool, len(matched))
	for _, r := range matched {
		got[r.ProviderUUID] = true
	}
	for _, c := range cases {
		if got[c.uuid] != c.want {
			t.Errorf("hasStoredModelList matched %s = %v, want %v", c.uuid, got[c.uuid], c.want)
		}
	}
}

func contains(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}
