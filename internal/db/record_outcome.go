package db

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/tingly-dev/tingly-box/internal/loadbalance"
)

// RecordRequestOutcome persists a service's updated stats (StatsStore) and a
// usage audit row (UsageStore) together in one SQLite transaction, instead
// of each store committing independently. Assumes both stores share the
// same *gorm.DB, true of every production call site (StoreManager wires
// every store to one shared connection). See .design/hot-path-db-access.md.
//
// service and/or usage may be nil (only one side has something to persist);
// statsStore and/or usageStore may be nil (store not initialized).
//
// Callers should not pass the inbound request's context here. This runs
// after the response is already decided -- including for a client that
// disconnected mid-request, which is itself a status this package records
// ("canceled"/"client_disconnected"). A context already canceled by that
// disconnect would make gorm refuse the write before it starts, silently
// dropping the exact audit row the cancellation should have produced. Pass
// context.Background(), or a context scoped to the server's own lifecycle
// (e.g. shutdown), not the request's.
func RecordRequestOutcome(ctx context.Context, statsStore *StatsStore, usageStore *UsageStore, service *loadbalance.Service, usage *UsageRecord) error {
	if statsStore == nil {
		if usageStore == nil || usage == nil {
			return nil
		}
		return usageStore.RecordUsage(ctx, usage)
	}
	if usageStore == nil {
		return statsStore.UpdateFromService(ctx, service)
	}

	// Build both records before taking any lock -- neither builder touches
	// store state, only its own argument.
	statsRecord := buildStatsRecordFromService(service)
	if usage != nil {
		prepareUsageRecord(usage)
	}

	statsStore.mu.Lock()
	defer statsStore.mu.Unlock()
	usageStore.mu.Lock()
	defer usageStore.mu.Unlock()

	return statsStore.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if statsRecord != nil {
			if err := tx.Save(statsRecord).Error; err != nil {
				return fmt.Errorf("failed to save service stats: %w", err)
			}
		}
		if usage != nil {
			if err := tx.Create(usage).Error; err != nil {
				return fmt.Errorf("failed to create usage record: %w", err)
			}
		}
		return nil
	})
}
