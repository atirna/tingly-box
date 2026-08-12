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
// stores are assumed to share the same *gorm.DB, true of every production
// call site -- StoreManager wires every store to one shared connection (see
// its initProviderStore/initAPITokenStore-style init* helpers) -- so the
// transaction below always covers both writes there.
func RecordRequestOutcome(statsStore *StatsStore, usageStore *UsageStore, service *loadbalance.Service, usage *UsageRecord) error {
	if statsStore == nil {
		if usageStore == nil || usage == nil {
			return nil
		}
		return usageStore.RecordUsage(usage)
	}
	if usageStore == nil {
		return statsStore.UpdateFromService(service)
	}

	// Build both records before taking any lock: buildStatsRecordFromService
	// only mutates the caller's *loadbalance.Service and prepareUsageRecord
	// only mutates its own argument, neither touches store state, so there's
	// no reason to hold statsStore.mu/usageStore.mu for this in-memory work.
	statsRecord := buildStatsRecordFromService(service)
	if usage != nil {
		prepareUsageRecord(usage)
	}

	// Lock order (stats then usage) is arbitrary but fixed, matching the only
	// other place these two stores could ever be locked together -- nowhere
	// else today, so there's no existing convention to conflict with.
	statsStore.mu.Lock()
	defer statsStore.mu.Unlock()
	usageStore.mu.Lock()
	defer usageStore.mu.Unlock()

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
