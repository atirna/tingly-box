package db

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tingly-dev/tingly-box/internal/loadbalance"
)

// reopenStoreManager closes sm and opens a fresh one over the same directory,
// so assertions see only what actually reached disk.
func reopenStoreManager(t *testing.T, sm *StoreManager, dir string) *StoreManager {
	t.Helper()
	require.NoError(t, sm.Close())
	reopened, err := NewStoreManager(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	return reopened
}

// usageRowCount returns how many usage rows are persisted.
func usageRowCount(t *testing.T, sm *StoreManager) int64 {
	t.Helper()
	_, total, err := sm.Usage().GetRecords(time.Time{}, time.Time{}, nil, 1, 0)
	require.NoError(t, err)
	return total
}

func anOutcome(provider string) *UsageRecord {
	return &UsageRecord{ProviderUUID: provider, ProviderName: provider, Model: "m1", Scenario: "openai"}
}

func TestRecordOutcome_PersistsViaWriter(t *testing.T) {
	sm, err := NewStoreManager(t.TempDir())
	require.NoError(t, err)
	defer sm.Close()

	service := &loadbalance.Service{Provider: "p1", Model: "m1", Active: true}
	require.NoError(t, sm.RecordOutcome(service, anOutcome("p1")))

	// The write is async: it lands on the next flush.
	require.Eventually(t, func() bool { return usageRowCount(t, sm) == 1 },
		5*time.Second, 10*time.Millisecond, "usage row never reached the table")

	_, found := sm.Stats().Get("p1", "m1")
	assert.True(t, found)
}

func TestRecordOutcome_CloseFlushesBufferedOutcomes(t *testing.T) {
	dir := t.TempDir()
	sm, err := NewStoreManager(dir)
	require.NoError(t, err)

	// Enqueue fewer outcomes than a full batch, then close immediately — well
	// inside the flush interval — so persistence can only have come from
	// Close's final drain.
	for i := 0; i < 10; i++ {
		provider := fmt.Sprintf("p%d", i)
		require.NoError(t, sm.RecordOutcome(
			&loadbalance.Service{Provider: provider, Model: "m1", Active: true},
			anOutcome(provider)))
	}

	reopened := reopenStoreManager(t, sm, dir)
	assert.EqualValues(t, 10, usageRowCount(t, reopened))
	for i := 0; i < 10; i++ {
		_, found := reopened.Stats().Get(fmt.Sprintf("p%d", i), "m1")
		assert.True(t, found, "stats row for p%d must survive close", i)
	}
}

// TestRecordOutcome_StatsCoalesceToNewestSnapshot: stats rows are cumulative
// snapshots, so repeated outcomes for one service collapse to the newest
// while every usage row is kept.
func TestRecordOutcome_StatsCoalesceToNewestSnapshot(t *testing.T) {
	dir := t.TempDir()
	sm, err := NewStoreManager(dir)
	require.NoError(t, err)

	service := &loadbalance.Service{Provider: "p1", Model: "m1", Active: true}
	for i := 0; i < 5; i++ {
		// RecordUsage advances the in-memory counters, so each snapshot is
		// strictly newer than the last.
		service.RecordUsage(10, 20)
		require.NoError(t, sm.RecordOutcome(service, anOutcome("p1")))
	}

	reopened := reopenStoreManager(t, sm, dir)
	stats, found := reopened.Stats().Get("p1", "m1")
	require.True(t, found)
	assert.EqualValues(t, 5, stats.RequestCount, "persisted stats must be the newest snapshot")
	assert.EqualValues(t, 5, usageRowCount(t, reopened), "every usage row must be kept")
}

// TestRecordOutcome_UsageQueueFullStillPersistsEverything: saturating the
// usage queue must cost nothing. Usage rows are written synchronously on
// overflow, and stats snapshots never queue at all — they coalesce into the
// stats store — so neither can be lost.
func TestRecordOutcome_UsageQueueFullStillPersistsEverything(t *testing.T) {
	dir := t.TempDir()
	sm, err := NewStoreManager(dir)
	require.NoError(t, err)

	service := &loadbalance.Service{Provider: "p1", Model: "m1", Active: true}

	// Block the flusher inside flush's stats lock so the queue cannot drain,
	// then push until it is genuinely full. Deriving the count instead of
	// hardcoding one keeps this deterministic no matter how many outcomes the
	// flusher absorbed into its in-flight batch before blocking.
	sm.Stats().mu.Lock()
	record := func() { service.RecordUsage(10, 20); require.NoError(t, sm.RecordOutcome(service, anOutcome("p1"))) }

	total := 0
	for len(sm.outcomeWriter.usageCh) < usageQueueSize {
		require.Less(t, total, 4*(usageQueueSize+outcomeMaxBatch), "queue never saturated")
		record()
		total++
	}
	// Now every further outcome takes the overflow path.
	for i := 0; i < 5; i++ {
		record()
		total++
	}
	require.Len(t, sm.outcomeWriter.usageCh, usageQueueSize, "queue must still be saturated")
	sm.Stats().mu.Unlock()

	reopened := reopenStoreManager(t, sm, dir)
	assert.EqualValues(t, total, usageRowCount(t, reopened),
		"no usage audit row may be dropped when the queue saturates")

	stats, found := reopened.Stats().Get("p1", "m1")
	require.True(t, found)
	assert.EqualValues(t, total, stats.RequestCount,
		"stats coalesce rather than queue, so saturation cannot lose a snapshot")
}

// TestRecordOutcome_ClearStatsIsNotResurrectedByPendingSnapshot: clearing
// stats must stick even when snapshots are still buffered. Before the stats
// dirty-set moved under the stats lock, a snapshot taken before the clear
// was committed a moment later and the cleared counters came back.
func TestRecordOutcome_ClearStatsSticksAgainstBufferedSnapshots(t *testing.T) {
	dir := t.TempDir()
	sm, err := NewStoreManager(dir)
	require.NoError(t, err)

	service := &loadbalance.Service{Provider: "p1", Model: "m1", Active: true}
	service.RecordUsage(10, 20)
	require.NoError(t, sm.RecordOutcome(service, anOutcome("p1")))

	// Clear while the snapshot is still buffered (well inside the flush
	// interval).
	require.NoError(t, sm.Stats().ClearAll())
	_, found := sm.Stats().Get("p1", "m1")
	assert.False(t, found, "cleared stats must not still be readable from the pending set")

	reopened := reopenStoreManager(t, sm, dir)
	_, found = reopened.Stats().Get("p1", "m1")
	assert.False(t, found, "a snapshot buffered before the clear must not resurrect the row")

	// The usage audit rows are unaffected by clearing stats.
	assert.EqualValues(t, 1, usageRowCount(t, reopened))
}

// TestStatsStore_ClearServiceDropsOnlyItsPendingSnapshot is the per-service
// counterpart to the ClearAll case above.
func TestStatsStore_ClearServiceDropsOnlyItsPendingSnapshot(t *testing.T) {
	dir := t.TempDir()
	sm, err := NewStoreManager(dir)
	require.NoError(t, err)

	for _, provider := range []string{"p1", "p2"} {
		svc := &loadbalance.Service{Provider: provider, Model: "m1", Active: true}
		svc.RecordUsage(10, 20)
		require.NoError(t, sm.RecordOutcome(svc, anOutcome(provider)))
	}

	require.NoError(t, sm.Stats().ClearService("p1", "m1"))

	reopened := reopenStoreManager(t, sm, dir)
	_, found := reopened.Stats().Get("p1", "m1")
	assert.False(t, found, "cleared service must stay cleared")
	_, found = reopened.Stats().Get("p2", "m1")
	assert.True(t, found, "clearing one service must not drop another's pending snapshot")
}

func TestRecordOutcome_AfterCloseFallsBackSafely(t *testing.T) {
	sm, err := NewStoreManager(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, sm.Close())

	// Stores are nil after Close; RecordOutcome must not panic.
	assert.NoError(t, sm.RecordOutcome(
		&loadbalance.Service{Provider: "p1", Model: "m1", Active: true}, anOutcome("p1")))
}
