package db

import (
	"context"
	"testing"
	"time"

	"github.com/tingly-dev/tingly-box/internal/loadbalance"
)

// newUsageStoreForTest builds a UsageStore over a fresh temp dir, closed when
// the test or benchmark finishes. Takes testing.TB so benchmarks and tests
// share one setup path.
func newUsageStoreForTest(tb testing.TB) *UsageStore {
	tb.Helper()
	store, err := NewUsageStore(tb.TempDir())
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = store.Close() })
	return store
}

// Benchmarks for StatsStore/UsageStore, both written synchronously once per
// completed LLM request by ProtocolHandler.trackUsageWithTokenUsage -- see
// .design/hot-path-db-access.md.
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
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.RecordUsage(10, 20)
		if err := store.UpdateFromService(ctx, service); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkUsageStore_RecordUsage exercises the exact call
// recordDetailedUsageWithTokenUsage makes: a single-row INSERT per
// completed request (the proxy's usage audit log).
func BenchmarkUsageStore_RecordUsage(b *testing.B) {
	store := newUsageStoreForTest(b)
	ctx := context.Background()

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
		if err := store.RecordUsage(ctx, record); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStatsAndUsage_Combined runs both writes via two separate stores
// (two independent transactions) -- the "before" baseline for
// BenchmarkStatsAndUsage_RecordRequestOutcome below.
func BenchmarkStatsAndUsage_Combined(b *testing.B) {
	statsStore, err := NewStatsStore(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}

	usageStore := newUsageStoreForTest(b)

	service := &loadbalance.Service{
		Provider: "bench-provider",
		Model:    "bench-model",
		Weight:   1,
		Active:   true,
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.RecordUsage(10, 20)
		if err := statsStore.UpdateFromService(ctx, service); err != nil {
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
		if err := usageStore.RecordUsage(ctx, record); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStatsAndUsage_RecordRequestOutcome wires both stores through
// StoreManager (shared *gorm.DB), so the two writes land in one transaction.
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
	ctx := context.Background()

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
		if err := RecordRequestOutcome(ctx, statsStore, usageStore, service, record); err != nil {
			b.Fatal(err)
		}
	}
}
