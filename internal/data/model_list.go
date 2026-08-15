package data

import (
	"context"
	"encoding/json"
	"time"

	"github.com/tingly-dev/tingly-box/internal/db"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// ModelCacheTTL is how long a cached model list is considered fresh.
// After this duration, GetModels returns empty so the caller re-fetches.
const ModelCacheTTL = time.Hour

// ModelListManager manages models for different providers using SQLite database
type ModelListManager struct {
	modelStore *db.ModelStore
}

// NewModelListManager wraps the StoreManager's ModelStore. There is
// deliberately no constructor that opens its own connection: this manager
// used to do exactly that, giving the process a second connection pool (and
// a second AutoMigrate over provider_models) against the same tingly.db
// StoreManager had just opened. Closing is the StoreManager's job.
func NewModelListManager(store *db.ModelStore) *ModelListManager {
	return &ModelListManager{modelStore: store}
}

// SaveModels saves models for a provider by UUID to the database.
// source should be db.ModelSourceAPI or db.ModelSourceTemplate.
func (mm *ModelListManager) SaveModels(ctx context.Context, provider *typ.Provider, models []string, source db.ModelSource) error {
	return mm.modelStore.SaveModels(ctx, provider, models, source)
}

// SaveModelsWithRaw saves a successful real upstream fetch (model list + raw
// payload) and clears any prior error fields.
func (mm *ModelListManager) SaveModelsWithRaw(ctx context.Context, provider *typ.Provider, models []string, source db.ModelSource, raw json.RawMessage) error {
	return mm.modelStore.SaveModelsWithRaw(ctx, provider, models, source, raw)
}

// SaveFetchFailure records a fetch error without clobbering an existing model
// list. raw is optional (the upstream body, when available).
func (mm *ModelListManager) SaveFetchFailure(ctx context.Context, provider *typ.Provider, lastErr string, raw json.RawMessage) error {
	return mm.modelStore.SaveFetchFailure(ctx, provider, lastErr, raw, time.Time{})
}

// GetModels returns models for a provider by reading from database.
// Returns empty if the cached record is older than ModelCacheTTL.
func (mm *ModelListManager) GetModels(ctx context.Context, uid string) []string {
	return mm.modelStore.GetModels(ctx, uid, ModelCacheTTL)
}

// GetAllProviders returns all provider UUIDs that have models
func (mm *ModelListManager) GetAllProviders(ctx context.Context) []string {
	return mm.modelStore.GetAllProviders(ctx)
}

// HasModels checks if a provider has models in the database
func (mm *ModelListManager) HasModels(ctx context.Context, providerUUID string) bool {
	return mm.modelStore.HasModels(ctx, providerUUID)
}

// RemoveProvider removes a provider's models from the database
func (mm *ModelListManager) RemoveProvider(ctx context.Context, providerUUID string) error {
	return mm.modelStore.RemoveProvider(ctx, providerUUID)
}

// GetProviderInfo returns basic info about a provider by reading from database
func (mm *ModelListManager) GetProviderInfo(ctx context.Context, uid string) (apiBase string, lastUpdated string, exists bool) {
	return mm.modelStore.GetProviderInfo(ctx, uid)
}

// GetFetchFailure returns the last recorded fetch error for a provider, if any.
func (mm *ModelListManager) GetFetchFailure(ctx context.Context, uid string) (string, bool) {
	return mm.modelStore.GetFetchFailure(ctx, uid)
}
