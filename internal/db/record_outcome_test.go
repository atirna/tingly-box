package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tingly-dev/tingly-box/internal/loadbalance"
)

func TestRecordRequestOutcome_SavesBoth(t *testing.T) {
	sm, err := NewStoreManager(t.TempDir())
	require.NoError(t, err)
	defer sm.Close()

	service := &loadbalance.Service{Provider: "p1", Model: "m1", Active: true}
	usage := &UsageRecord{ProviderUUID: "p1", ProviderName: "p1", Model: "m1", Scenario: "openai"}

	require.NoError(t, RecordRequestOutcome(sm.Stats(), sm.Usage(), service, usage))

	_, found := sm.Stats().Get("p1", "m1")
	assert.True(t, found)

	records, total, err := sm.Usage().GetRecords(usage.Timestamp.Add(-1), usage.Timestamp.Add(1), nil, 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, records, 1)
	assert.Equal(t, "p1", records[0].ProviderUUID)
}

func TestRecordRequestOutcome_NilStatsStore(t *testing.T) {
	sm, err := NewStoreManager(t.TempDir())
	require.NoError(t, err)
	defer sm.Close()

	usage := &UsageRecord{ProviderUUID: "p1", ProviderName: "p1", Model: "m1", Scenario: "openai"}
	require.NoError(t, RecordRequestOutcome(nil, sm.Usage(), nil, usage))

	_, total, err := sm.Usage().GetRecords(usage.Timestamp.Add(-1), usage.Timestamp.Add(1), nil, 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
}

func TestRecordRequestOutcome_NilUsageStore(t *testing.T) {
	sm, err := NewStoreManager(t.TempDir())
	require.NoError(t, err)
	defer sm.Close()

	service := &loadbalance.Service{Provider: "p1", Model: "m1", Active: true}
	require.NoError(t, RecordRequestOutcome(sm.Stats(), nil, service, nil))

	_, found := sm.Stats().Get("p1", "m1")
	assert.True(t, found)
}

// TestRecordRequestOutcome_NilStatsStore_NilUsage covers the branch where
// statsStore is nil and usage is also nil: RecordUsage(nil) errors ("record
// cannot be nil"), so this must short-circuit before calling it rather than
// surfacing that as a spurious failure for a legitimate no-op call.
func TestRecordRequestOutcome_NilStatsStore_NilUsage(t *testing.T) {
	sm, err := NewStoreManager(t.TempDir())
	require.NoError(t, err)
	defer sm.Close()

	assert.NoError(t, RecordRequestOutcome(nil, sm.Usage(), nil, nil))
}

// TestRecordRequestOutcome_DifferentDB_NilUsage covers the same "nil usage
// must not reach UsageStore.RecordUsage" requirement on the statsStore.db !=
// usageStore.db fallback path (stores backed by independently constructed
// *gorm.DBs, e.g. built via NewStatsStore/NewUsageStore directly rather than
// StoreManager).
func TestRecordRequestOutcome_DifferentDB_NilUsage(t *testing.T) {
	statsStore, err := NewStatsStore(t.TempDir())
	require.NoError(t, err)
	usageStore, err := NewUsageStore(t.TempDir())
	require.NoError(t, err)

	service := &loadbalance.Service{Provider: "p1", Model: "m1", Active: true}
	assert.NoError(t, RecordRequestOutcome(statsStore, usageStore, service, nil))

	_, found := statsStore.Get("p1", "m1")
	assert.True(t, found)
}

// TestRecordRequestOutcome_AtomicRollback confirms the stats write and the
// usage write share one transaction: a usage row that fails to persist (a
// deliberate primary-key collision here) must roll back the stats write from
// the same call too, not leave it half-committed.
func TestRecordRequestOutcome_AtomicRollback(t *testing.T) {
	sm, err := NewStoreManager(t.TempDir())
	require.NoError(t, err)
	defer sm.Close()

	statsStore, usageStore := sm.Stats(), sm.Usage()
	require.NoError(t, usageStore.RecordUsage(&UsageRecord{ID: 999, ProviderUUID: "seed", ProviderName: "seed", Model: "seed", Scenario: "openai"}))

	service := &loadbalance.Service{Provider: "p1", Model: "m1", Active: true}
	conflicting := &UsageRecord{ID: 999, ProviderUUID: "p1", ProviderName: "p1", Model: "m1", Scenario: "openai"}

	err = RecordRequestOutcome(statsStore, usageStore, service, conflicting)
	assert.Error(t, err)

	_, found := statsStore.Get("p1", "m1")
	assert.False(t, found, "stats write from the same call must have rolled back with the failed usage write")
}
