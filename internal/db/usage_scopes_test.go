package db

import (
	"strings"
	"testing"
	"time"
)

func newUsageStoreForScopes(t *testing.T) *UsageStore {
	t.Helper()
	store, err := NewUsageStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewUsageStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// TestUsageFilterColumnValidation covers the whitelist on every entry point
// that takes a caller-supplied filter map. The map's key is interpolated into
// SQL, so an unknown key must be refused rather than reaching the driver.
func TestUsageFilterColumnValidation(t *testing.T) {
	store := newUsageStoreForScopes(t)
	seedUsageRecords(t, store, 2)

	start := time.Now().Add(-72 * time.Hour)
	end := time.Now()

	t.Run("accepts known columns", func(t *testing.T) {
		ok := map[string]string{"provider_uuid": "prov-a", "model": "model-x", "user_id": "admin"}

		if _, _, err := store.GetRecords(start, end, ok, 10, 0); err != nil {
			t.Errorf("GetRecords: %v", err)
		}
		if _, err := store.GetTimeSeries("hour", start, end, ok); err != nil {
			t.Errorf("GetTimeSeries: %v", err)
		}
		if _, err := store.GetPerformanceSummary(start, end, ok); err != nil {
			t.Errorf("GetPerformanceSummary: %v", err)
		}
	})

	t.Run("rejects an unknown column", func(t *testing.T) {
		bad := map[string]string{"provider_uuid": "prov-a", "not_a_column": "x"}

		if _, _, err := store.GetRecords(start, end, bad, 10, 0); err == nil {
			t.Error("GetRecords accepted an unknown filter column")
		} else if !strings.Contains(err.Error(), "not_a_column") {
			t.Errorf("GetRecords error = %v, want it to name the column", err)
		}
		if _, err := store.GetTimeSeries("hour", start, end, bad); err == nil {
			t.Error("GetTimeSeries accepted an unknown filter column")
		}
		if _, err := store.GetPerformanceSummary(start, end, bad); err == nil {
			t.Error("GetPerformanceSummary accepted an unknown filter column")
		}
	})

	// The whole point of the whitelist: a key carrying SQL must not be
	// interpolated. It has to be refused before it reaches the driver.
	t.Run("rejects a key carrying SQL", func(t *testing.T) {
		injected := map[string]string{"1=1 OR user_id": "admin"}
		if _, _, err := store.GetRecords(start, end, injected, 10, 0); err == nil {
			t.Error("GetRecords accepted a filter key carrying SQL")
		}
	})

	// An unknown key must not be silently dropped: dropping a user_id filter
	// would widen the result set to other users' records.
	t.Run("an unknown key is an error, not a dropped filter", func(t *testing.T) {
		scoped, _, err := store.GetRecords(start, end, map[string]string{"user_id": "admin"}, 1000, 0)
		if err != nil {
			t.Fatalf("GetRecords: %v", err)
		}
		unscoped, _, err := store.GetRecords(start, end, nil, 1000, 0)
		if err != nil {
			t.Fatalf("GetRecords: %v", err)
		}
		if len(scoped) >= len(unscoped) {
			t.Fatalf("test seed is not discriminating: scoped=%d unscoped=%d", len(scoped), len(unscoped))
		}
		if _, _, err := store.GetRecords(start, end, map[string]string{"user_idd": "admin"}, 1000, 0); err == nil {
			t.Error("a typo'd filter key was accepted, which would widen the result set")
		}
	})
}

// TestUsageFilterScopesPreserveResults pins that routing the four call sites
// through shared scopes did not change what they return.
func TestUsageFilterScopesPreserveResults(t *testing.T) {
	store := newUsageStoreForScopes(t)
	seedUsageRecords(t, store, 3)

	start := time.Now().Add(-96 * time.Hour)
	end := time.Now()
	filters := map[string]string{"provider_uuid": "prov-a", "user_id": "admin"}

	t.Run("GetRecords honours every filter", func(t *testing.T) {
		records, total, err := store.GetRecords(start, end, filters, 1000, 0)
		if err != nil {
			t.Fatalf("GetRecords: %v", err)
		}
		if len(records) == 0 {
			t.Fatal("no records matched; seed does not exercise the filter")
		}
		if total < int64(len(records)) {
			t.Errorf("total %d < returned %d", total, len(records))
		}
		for _, r := range records {
			if r.ProviderUUID != "prov-a" || r.UserID != "admin" {
				t.Fatalf("filter not applied: got provider=%q user=%q", r.ProviderUUID, r.UserID)
			}
		}
	})

	t.Run("stats filters match the map form", func(t *testing.T) {
		viaStruct, err := store.GetAggregatedStats(UsageStatsQuery{
			GroupBy:   "model",
			StartTime: start,
			EndTime:   end,
			Provider:  "prov-a",
			UserID:    "admin",
		})
		if err != nil {
			t.Fatalf("GetAggregatedStats: %v", err)
		}

		records, _, err := store.GetRecords(start, end, filters, 100000, 0)
		if err != nil {
			t.Fatalf("GetRecords: %v", err)
		}

		var statsTotal int64
		for _, s := range viaStruct {
			statsTotal += s.RequestCount
		}
		if statsTotal != int64(len(records)) {
			t.Errorf("aggregated request count %d != %d records matching the same filter",
				statsTotal, len(records))
		}
	})

	t.Run("repeated calls are stable", func(t *testing.T) {
		// Map iteration order is randomized in Go; the scope sorts keys so the
		// generated SQL (and therefore the plan) is the same every call.
		first, _, err := store.GetRecords(start, end, filters, 1000, 0)
		if err != nil {
			t.Fatalf("GetRecords: %v", err)
		}
		for i := 0; i < 20; i++ {
			again, _, err := store.GetRecords(start, end, filters, 1000, 0)
			if err != nil {
				t.Fatalf("GetRecords iteration %d: %v", i, err)
			}
			if len(again) != len(first) {
				t.Fatalf("iteration %d returned %d records, first returned %d", i, len(again), len(first))
			}
		}
	})
}

// TestDailyFilterColumnValidation covers the usage_daily subset, which has
// fewer dimension columns than usage_records.
func TestDailyFilterColumnValidation(t *testing.T) {
	if err := validateDailyFilterColumns(map[string]string{"provider_uuid": "a", "model": "m", "user_id": "u"}); err != nil {
		t.Errorf("daily columns rejected: %v", err)
	}
	// Real usage_records columns that usage_daily does not carry.
	for _, key := range []string{"scenario", "rule_uuid", "status"} {
		if err := validateDailyFilterColumns(map[string]string{key: "x"}); err == nil {
			t.Errorf("validateDailyFilterColumns accepted %q, which usage_daily has no column for", key)
		}
		// The same key is fine against the raw table.
		if err := validateFilterColumns(map[string]string{key: "x"}); err != nil {
			t.Errorf("validateFilterColumns rejected %q, which usage_records does have: %v", key, err)
		}
	}
}

// TestStatsQueryFilterMap pins the typed-to-map rendering, including that an
// unset field produces no filter at all.
func TestStatsQueryFilterMap(t *testing.T) {
	full := UsageStatsQuery{
		Provider: "p", Model: "m", Scenario: "s",
		RuleUUID: "r", UserID: "u", Status: "success",
	}
	got := full.filterMap()
	want := map[string]string{
		"provider_uuid": "p", "model": "m", "scenario": "s",
		"rule_uuid": "r", "user_id": "u", "status": "success",
	}
	if len(got) != len(want) {
		t.Fatalf("filterMap = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("filterMap[%q] = %q, want %q", k, got[k], v)
		}
	}
	// Every key it can emit must be in the whitelist.
	if err := validateFilterColumns(got); err != nil {
		t.Errorf("filterMap emitted a non-whitelisted column: %v", err)
	}

	if got := (UsageStatsQuery{}).filterMap(); len(got) != 0 {
		t.Errorf("empty query filterMap = %v, want no filters", got)
	}

	daily := full.dailyFilterMap()
	if len(daily) != 3 {
		t.Errorf("dailyFilterMap = %v, want only the 3 usage_daily dimensions", daily)
	}
	if err := validateDailyFilterColumns(daily); err != nil {
		t.Errorf("dailyFilterMap emitted a column usage_daily lacks: %v", err)
	}
}
