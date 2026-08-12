package db

import (
	"testing"
	"time"

	"github.com/tingly-dev/tingly-box/internal/loadbalance"
)

// newUsageStoreForBench builds a UsageStore for a fresh temp dir.
// NewUsageStore is never called in production (StoreManager builds
// UsageStore directly over its own already-open *gorm.DB instead), so this
// benchmark suite is its only real caller -- see NewUsageStore's directory
// setup, which this relies on.
func newUsageStoreForBench(b *testing.B) *UsageStore {
	b.Helper()
	store, err := NewUsageStore(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	return store
}

// Benchmarks for the two stores that internal/protocolserver/usage_tracking.go
// writes to synchronously, once per completed LLM request (both streaming and
// non-streaming, success and error paths), before the response is handed back
// to the client -- see ProtocolHandler.trackUsageWithTokenUsage, which calls
// updateServiceStats (-> StatsStore.UpdateFromService) and
// recordDetailedUsageWithTokenUsage (-> UsageStore.RecordUsage) inline in the
// request goroutine. Unlike ProviderStore.GetByUUID or
// APITokenStore.ValidateToken, these are writes, not reads that can be
// served from a cache -- there's a real row to persist every request. See
// .design/hot-path-db-access.md for the read-path fixes this follows up on.
//
// Run: go test ./internal/db/... -bench 'StatsStore|UsageStore' -benchmem -run '^$'

// BenchmarkStatsStore_UpdateFromService exercises the exact call
// updateServiceStats makes: an upsert (gorm Save, no prior read) of a
// service's current in-memory stats snapshot.
func BenchmarkStatsStore_UpdateFromService(b *testing.B) {
	store, err := NewStatsStore(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}

	service := &loadbalance.Service{
		Provider: "bench-provider",
		Model:    "bench-model",
		Weight:   1,
		Active:   true,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.RecordUsage(10, 20)
		if err := store.UpdateFromService(service); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkUsageStore_RecordUsage exercises the exact call
// recordDetailedUsageWithTokenUsage makes: a single-row INSERT per
// completed request (the proxy's usage audit log).
func BenchmarkUsageStore_RecordUsage(b *testing.B) {
	store := newUsageStoreForBench(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		record := &UsageRecord{
			ProviderUUID: "bench-provider",
			ProviderName: "bench-provider",
			Model:        "bench-model",
			Scenario:     "openai",
			RuleUUID:     "bench-rule",
			UserID:       DefaultAdminUserID,
			RequestModel: "bench-model",
			Timestamp:    time.Now(),
			InputTokens:  10,
			OutputTokens: 20,
			Status:       "success",
			LatencyMs:    120,
		}
		if err := store.RecordUsage(record); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStatsAndUsage_Combined runs both writes back to back via two
// separate stores (each with its own *gorm.DB and its own implicit
// transaction) -- this was the only shape available before
// RecordRequestOutcome and stays here as the "before" baseline for
// BenchmarkStatsAndUsage_RecordRequestOutcome below to be compared against.
func BenchmarkStatsAndUsage_Combined(b *testing.B) {
	statsStore, err := NewStatsStore(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}

	usageStore := newUsageStoreForBench(b)

	service := &loadbalance.Service{
		Provider: "bench-provider",
		Model:    "bench-model",
		Weight:   1,
		Active:   true,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.RecordUsage(10, 20)
		if err := statsStore.UpdateFromService(service); err != nil {
			b.Fatal(err)
		}
		record := &UsageRecord{
			ProviderUUID: "bench-provider",
			ProviderName: "bench-provider",
			Model:        "bench-model",
			Scenario:     "openai",
			RuleUUID:     "bench-rule",
			UserID:       DefaultAdminUserID,
			RequestModel: "bench-model",
			Timestamp:    time.Now(),
			InputTokens:  10,
			OutputTokens: 20,
			Status:       "success",
			LatencyMs:    120,
		}
		if err := usageStore.RecordUsage(record); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStatsAndUsage_RecordRequestOutcome exercises RecordRequestOutcome
// with both stores wired exactly as production does -- sharing one *gorm.DB
// via StoreManager, the precondition for the two writes to land in a single
// SQLite transaction/commit instead of BenchmarkStatsAndUsage_Combined's two.
func BenchmarkStatsAndUsage_RecordRequestOutcome(b *testing.B) {
	sm, err := NewStoreManager(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = sm.Close() })

	statsStore, usageStore := sm.Stats(), sm.Usage()

	service := &loadbalance.Service{
		Provider: "bench-provider",
		Model:    "bench-model",
		Weight:   1,
		Active:   true,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.RecordUsage(10, 20)
		record := &UsageRecord{
			ProviderUUID: "bench-provider",
			ProviderName: "bench-provider",
			Model:        "bench-model",
			Scenario:     "openai",
			RuleUUID:     "bench-rule",
			UserID:       DefaultAdminUserID,
			RequestModel: "bench-model",
			Timestamp:    time.Now(),
			InputTokens:  10,
			OutputTokens: 20,
			Status:       "success",
			LatencyMs:    120,
		}
		if err := RecordRequestOutcome(statsStore, usageStore, service, record); err != nil {
			b.Fatal(err)
		}
	}
}
