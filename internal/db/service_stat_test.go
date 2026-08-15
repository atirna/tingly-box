package db

import (
	"context"
	"testing"

	"github.com/tingly-dev/tingly-box/internal/loadbalance"
)

// newTestStatsStore creates a stats store backed by a temp database.
func newTestStatsStore(t *testing.T) *StatsStore {
	t.Helper()
	sm, err := NewStoreManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewStoreManager error: %v", err)
	}
	t.Cleanup(func() { sm.Close() })
	return sm.Stats()
}

// TestStatsStore_ClearService verifies ClearService removes only the targeted
// provider:model, leaving other services' persisted stats intact. It fills the
// store with a couple of services, clears one, and asserts the survivor stays.
func TestStatsStore_ClearService(t *testing.T) {
	store := newTestStatsStore(t)
	ctx := context.Background()

	// Seed two services via the store's only remaining write path.
	svcA := &loadbalance.Service{Provider: "prov-a", Model: "m"}
	svcB := &loadbalance.Service{Provider: "prov-b", Model: "m"}
	svcA.RecordUsage(10, 20)
	svcB.RecordUsage(30, 40)
	if err := store.UpdateFromService(ctx, svcA); err != nil {
		t.Fatalf("UpdateFromService A: %v", err)
	}
	if err := store.UpdateFromService(ctx, svcB); err != nil {
		t.Fatalf("UpdateFromService B: %v", err)
	}

	// Clear only A.
	if err := store.ClearService(ctx, "prov-a", "m"); err != nil {
		t.Fatalf("ClearService: %v", err)
	}

	// A is gone.
	if _, ok := store.Get(ctx, "prov-a", "m"); ok {
		t.Fatal("prov-a/m should be cleared")
	}
	// B survives.
	gotB, ok := store.Get(ctx, "prov-b", "m")
	if !ok {
		t.Fatal("prov-b/m should still exist (ClearService must be scoped)")
	}
	if gotB.RequestCount != 1 {
		t.Errorf("prov-b/m RequestCount = %d, want 1 (untouched)", gotB.RequestCount)
	}

	// Clearing a non-existent service is a no-op (no error).
	if err := store.ClearService(ctx, "never", "existed"); err != nil {
		t.Fatalf("ClearService on missing row should be a no-op, got: %v", err)
	}
}
