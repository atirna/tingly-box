package db

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tingly-dev/tingly-box/internal/loadbalance"
)

// waitForUsageTotal polls until the usage table reports want rows (the
// writer flushes asynchronously) or the timeout elapses.
func waitForUsageTotal(t *testing.T, sm *StoreManager, want int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, total, err := sm.Usage().GetRecords(time.Time{}, time.Time{}, nil, 1, 0)
		require.NoError(t, err)
		if total == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("usage table never reached %d rows", want)
}

func TestRecordOutcome_PersistsViaWriter(t *testing.T) {
	sm, err := NewStoreManager(t.TempDir())
	require.NoError(t, err)
	defer sm.Close()

	service := &loadbalance.Service{Provider: "p1", Model: "m1", Active: true}
	usage := &UsageRecord{ProviderUUID: "p1", ProviderName: "p1", Model: "m1", Scenario: "openai"}

	require.NoError(t, sm.RecordOutcome(service, usage))

	// The write is async: it lands on the next flush tick.
	waitForUsageTotal(t, sm, 1)
	_, found := sm.Stats().Get("p1", "m1")
	assert.True(t, found)
}

func TestRecordOutcome_CloseFlushesBufferedOutcomes(t *testing.T) {
	dir := t.TempDir()
	sm, err := NewStoreManager(dir)
	require.NoError(t, err)

	// Enqueue fewer outcomes than a full batch, then close immediately —
	// well inside the flush interval — so persistence can only have come
	// from Close's final drain+flush.
	for i := 0; i < 10; i++ {
		service := &loadbalance.Service{Provider: fmt.Sprintf("p%d", i), Model: "m", Active: true}
		usage := &UsageRecord{ProviderUUID: service.Provider, ProviderName: service.Provider, Model: "m", Scenario: "openai"}
		require.NoError(t, sm.RecordOutcome(service, usage))
	}
	require.NoError(t, sm.Close())

	reopened, err := NewStoreManager(dir)
	require.NoError(t, err)
	defer reopened.Close()

	_, total, err := reopened.Usage().GetRecords(time.Time{}, time.Time{}, nil, 100, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 10, total)
	for i := 0; i < 10; i++ {
		_, found := reopened.Stats().Get(fmt.Sprintf("p%d", i), "m")
		assert.True(t, found, "stats row for p%d must survive close", i)
	}
}

// TestRecordOutcome_StatsDedupeKeepsNewestSnapshot: multiple outcomes for
// the same provider:model within one batch save only the newest cumulative
// snapshot, while every usage row is kept.
func TestRecordOutcome_StatsDedupeKeepsNewestSnapshot(t *testing.T) {
	dir := t.TempDir()
	sm, err := NewStoreManager(dir)
	require.NoError(t, err)

	service := &loadbalance.Service{Provider: "p1", Model: "m1", Active: true}
	for i := 0; i < 5; i++ {
		// RecordUsage advances the service's in-memory counters; each
		// enqueued snapshot is therefore strictly newer.
		service.RecordUsage(10, 20)
		usage := &UsageRecord{ProviderUUID: "p1", ProviderName: "p1", Model: "m1", Scenario: "openai"}
		require.NoError(t, sm.RecordOutcome(service, usage))
	}
	require.NoError(t, sm.Close())

	reopened, err := NewStoreManager(dir)
	require.NoError(t, err)
	defer reopened.Close()

	stats, found := reopened.Stats().Get("p1", "m1")
	require.True(t, found)
	assert.EqualValues(t, 5, stats.RequestCount, "persisted stats must be the newest snapshot")

	_, total, err := reopened.Usage().GetRecords(time.Time{}, time.Time{}, nil, 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 5, total, "every usage row must be kept")
}

func TestRecordOutcome_AfterCloseFallsBackSafely(t *testing.T) {
	sm, err := NewStoreManager(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, sm.Close())

	service := &loadbalance.Service{Provider: "p1", Model: "m1", Active: true}
	// Stores are nil after Close; RecordOutcome must not panic.
	assert.NoError(t, sm.RecordOutcome(service, &UsageRecord{ProviderUUID: "p1", ProviderName: "p1", Model: "m1", Scenario: "openai"}))
}
