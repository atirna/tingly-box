package db

import (
	"fmt"

	"gorm.io/gorm"

	"github.com/tingly-dev/tingly-box/internal/loadbalance"
)

// RecordRequestOutcome persists a service's updated stats (StatsStore) and a
// usage audit row (UsageStore) together, in a single SQLite transaction,
// instead of each store committing independently -- halving the per-request
// COMMIT count on the path ProtocolHandler.trackUsageWithTokenUsage /
// trackUsageFromContext take before responding to the client. See
// .design/hot-path-db-access.md, which also has the pprof/benchmark
// evidence this follows up on (BenchmarkStatsAndUsage_Combined).
//
// service and/or usage may be nil (only one side has something to persist);
// statsStore and/or usageStore may be nil (store not initialized). Both
// stores share the same *gorm.DB in production -- StoreManager wires every
// store to one shared connection -- so this is always able to combine the
// two writes there. The db != db fallback exists only for direct NewXStore
// construction (outside StoreManager, e.g. a future standalone tool),
// which is never mixed with StoreManager-built stores in production today.
func RecordRequestOutcome(statsStore *StatsStore, usageStore *UsageStore, service *loadbalance.Service, usage *UsageRecord) error {
	if statsStore == nil {
		if usageStore == nil {
			return nil
		}
		return usageStore.RecordUsage(usage)
	}
	if usageStore == nil {
		return statsStore.UpdateFromService(service)
	}

	if statsStore.db != usageStore.db {
		statsErr := statsStore.UpdateFromService(service)
		usageErr := usageStore.RecordUsage(usage)
		if statsErr != nil {
			return statsErr
		}
		return usageErr
	}

	// Lock order (stats then usage) is arbitrary but fixed, matching the only
	// other place these two stores could ever be locked together -- nowhere
	// else today, so there's no existing convention to conflict with.
	statsStore.mu.Lock()
	defer statsStore.mu.Unlock()
	usageStore.mu.Lock()
	defer usageStore.mu.Unlock()

	statsRecord := buildStatsRecordFromService(service)
	if usage != nil {
		prepareUsageRecord(usage)
	}

	return statsStore.db.Transaction(func(tx *gorm.DB) error {
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
