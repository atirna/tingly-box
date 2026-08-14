package utils

import (
	"path/filepath"
	"testing"
)

// TestProtectedCredentialStore_CloseReleasesConnection: the store opens its
// SQLite connection lazily and its owner is expected to release it. With WAL
// enabled a live connection holds three descriptors, so an owner that cannot
// close leaks all three for the process lifetime.
func TestProtectedCredentialStore_CloseReleasesConnection(t *testing.T) {
	store := NewProtectedCredentialStore(filepath.Join(t.TempDir(), "db", "guardrails.db"))

	if _, err := store.List(); err != nil {
		t.Fatalf("List (which opens the connection) failed: %v", err)
	}
	conn := store.db
	if conn == nil {
		t.Fatal("List should have opened a connection")
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if store.db != nil {
		t.Error("Close should clear the handle")
	}

	sqlDB, err := conn.DB()
	if err != nil {
		t.Fatalf("failed to reach the underlying handle: %v", err)
	}
	if err := sqlDB.Ping(); err == nil {
		t.Error("connection should be closed after Close")
	}
}

// TestProtectedCredentialStore_CloseIsSafeWhenUnused: Close must not fail on
// a store whose connection was never opened, or on a second call — its owner
// (Config.CloseStores) is documented as safe to call more than once.
func TestProtectedCredentialStore_CloseIsSafeWhenUnused(t *testing.T) {
	store := NewProtectedCredentialStore(filepath.Join(t.TempDir(), "db", "guardrails.db"))
	if err := store.Close(); err != nil {
		t.Fatalf("Close on an unused store failed: %v", err)
	}

	if _, err := store.List(); err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
}
