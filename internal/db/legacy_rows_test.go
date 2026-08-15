package db

import "testing"

// seedLegacyRows creates a table from T -- a narrower, pre-migration shape of
// one of this package's records -- and writes rows through it, then closes the
// handle so the caller can reopen the same file through the real store.
//
// The store's own AutoMigrate adds whatever columns T omits, which is what
// makes the reopened rows a faithful stand-in for a database written before
// the migration under test.
//
// Closing here is required, not tidiness: the reopen has to see the schema
// this handle committed.
func seedLegacyRows[T any](t *testing.T, dir string, rows []T) {
	t.Helper()

	db, err := openTinglyDB(dir)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	if err := db.AutoMigrate(new(T)); err != nil {
		t.Fatalf("migrate legacy schema: %v", err)
	}
	for i := range rows {
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatalf("seed legacy row %d: %v", i, err)
		}
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("legacy db handle: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}
}
