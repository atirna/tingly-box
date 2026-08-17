package db

import (
	"testing"
	"time"
)

// TestUsageRecordPersistsTraceID guards the trace_id column end to end: it is
// created by AutoMigrate, written by RecordUsage, and read back intact.
//
// The column is the only link from a billing row to the request's trace, and
// nothing else in the suite would notice its absence — a schema refactor that
// dropped it would leave every other usage test green while the WebUI quietly
// stopped being able to jump from a usage row to its trace.
func TestUsageRecordPersistsTraceID(t *testing.T) {
	store := newUsageStoreForTest(t)

	const traceID = "a1b2c3d4e5f60718293a4b5c6d7e8f90"
	if err := store.RecordUsage(&UsageRecord{
		ProviderUUID: "prov-a",
		ProviderName: "Acme",
		Model:        "model-x",
		Scenario:     "openai",
		UserID:       "admin",
		Status:       "success",
		InputTokens:  10,
		OutputTokens: 20,
		TotalTokens:  30,
		TraceID:      traceID,
	}); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}

	records, _, err := store.GetRecords(time.Now().Add(-time.Hour), time.Now().Add(time.Hour), nil, 10, 0)
	if err != nil {
		t.Fatalf("GetRecords: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].TraceID != traceID {
		t.Errorf("TraceID = %q, want %q", records[0].TraceID, traceID)
	}
}

// TestUsageRecordTraceIDOptional covers the common case: tracing disabled or
// the span unsampled leaves the column empty rather than failing the write.
func TestUsageRecordTraceIDOptional(t *testing.T) {
	store := newUsageStoreForTest(t)

	if err := store.RecordUsage(&UsageRecord{
		ProviderUUID: "prov-a",
		ProviderName: "Acme",
		Model:        "model-x",
		Scenario:     "openai",
		UserID:       "admin",
		Status:       "success",
	}); err != nil {
		t.Fatalf("RecordUsage without a trace id: %v", err)
	}

	records, _, err := store.GetRecords(time.Now().Add(-time.Hour), time.Now().Add(time.Hour), nil, 10, 0)
	if err != nil {
		t.Fatalf("GetRecords: %v", err)
	}
	if len(records) != 1 || records[0].TraceID != "" {
		t.Errorf("expected one record with an empty TraceID, got %+v", records)
	}
}
