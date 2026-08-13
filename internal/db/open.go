package db

import (
	"fmt"
	"os"
	"path/filepath"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/tingly-dev/tingly-box/internal/constant"
)

// defaultBusyTimeoutMs is the SQLite busy_timeout applied to every connection
// this package opens. StoreManager makes it configurable; the standalone
// NewXStore constructors use the default.
const defaultBusyTimeoutMs = 5000

// OpenSQLite opens a GORM handle on dbPath with the canonical options for
// tingly's SQLite databases — WAL journaling (which also forces
// synchronous=NORMAL in mattn/go-sqlite3), foreign keys on, and a busy
// timeout so concurrent connections back off instead of failing with
// SQLITE_BUSY — creating the parent directory first. A busyTimeoutMs of 0
// takes the default.
//
// This is the only place the option set is spelled out. It is exported for
// databases outside this package that are nonetheless tingly's own (the
// guardrails credential store); everything on tingly.db should go through
// StoreManager instead.
func OpenSQLite(dbPath string, busyTimeoutMs int) (*gorm.DB, error) {
	if busyTimeoutMs <= 0 {
		busyTimeoutMs = defaultBusyTimeoutMs
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}
	dsn := fmt.Sprintf("%s?_busy_timeout=%d&_journal_mode=WAL&_foreign_keys=1", dbPath, busyTimeoutMs)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open database %s: %w", dbPath, err)
	}
	return db, nil
}

// openTinglyDB opens the shared tingly.db under baseDir for the standalone
// NewXStore constructors. StoreManager opens the same file itself so it can
// thread its configurable busy timeout through.
//
// Creating baseDir is left to OpenSQLite, which creates baseDir/db and so
// baseDir along with it.
func openTinglyDB(baseDir string) (*gorm.DB, error) {
	return OpenSQLite(constant.GetDBFile(baseDir), defaultBusyTimeoutMs)
}

// storeConn is the connection half every store embeds: the *gorm.DB it runs
// on, plus whether the store opened that connection itself (standalone
// NewXStore constructors) or borrowed StoreManager's shared handle. Close is
// a no-op for borrowed connections, so calling Close on a store can never
// tear down the shared database out from under the other stores — a real
// hazard in the previous per-store Close implementations.
type storeConn struct {
	db     *gorm.DB
	ownsDB bool
}

// borrowedConn wraps a shared *gorm.DB the store must not close.
func borrowedConn(db *gorm.DB) storeConn {
	return storeConn{db: db}
}

// ownedConn wraps a *gorm.DB the store opened and is responsible for closing.
func ownedConn(db *gorm.DB) storeConn {
	return storeConn{db: db, ownsDB: true}
}

// Close closes the underlying connection if this store owns it; otherwise it
// is a no-op (the owner — usually StoreManager — closes it).
func (c *storeConn) Close() error {
	if !c.ownsDB || c.db == nil {
		return nil
	}
	sqlDB, err := c.db.DB()
	if err != nil {
		return err
	}
	// Leave c.db in place: GORM calls on a closed connection fail with a
	// clean "database is closed" error rather than a nil-pointer panic.
	return sqlDB.Close()
}
