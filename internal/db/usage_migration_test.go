package db

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// openLegacyUsageDB builds a usage_records table in its pre-split shape: the
// real current schema with the two cache columns renamed back to
// cache_read_input_tokens / cache_creation_input_tokens. Going through
// AutoMigrate and then renaming (rather than hand-writing DDL) keeps the
// table byte-for-byte GORM-shaped, which is what a real legacy database is.
func openLegacyUsageDB(t *testing.T) *gorm.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	conn, err := gorm.Open(sqlite.Open(dbPath+"?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=1"),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, conn.AutoMigrate(&UsageRecord{}))

	require.NoError(t, conn.Exec(
		`ALTER TABLE usage_records RENAME COLUMN cache_input_tokens TO cache_read_input_tokens`).Error)
	require.NoError(t, conn.Exec(
		`ALTER TABLE usage_records RENAME COLUMN cache_write_tokens TO cache_creation_input_tokens`).Error)

	return conn
}

// TestEnsureUsageRecordSchema_SplitsLegacyCacheColumns pins the mapping of
// the destructive cache-column migration: reads and writes must each land in
// their own column. The previous version summed both into the read column
// and then dropped the sources, silently rebooking cache writes as reads.
func TestEnsureUsageRecordSchema_SplitsLegacyCacheColumns(t *testing.T) {
	conn := openLegacyUsageDB(t)

	require.NoError(t, conn.Exec(`
		INSERT INTO usage_records
			(provider_uuid, provider_name, model, scenario, user_id, timestamp,
			 input_tokens, output_tokens, total_tokens,
			 cache_creation_input_tokens, cache_read_input_tokens, status)
		VALUES ('p1','p1','m1','openai','u1',?,10,20,30,7,13,'success')`,
		time.Now()).Error)

	require.NoError(t, conn.AutoMigrate(&UsageRecord{}))
	require.NoError(t, ensureUsageRecordSchema(conn))

	var got UsageRecord
	require.NoError(t, conn.First(&got).Error)
	assert.Equal(t, 13, got.CacheReadTokens, "cache_read_input_tokens must land in the read column")
	assert.Equal(t, 7, got.CacheWriteTokens, "cache_creation_input_tokens must land in the write column")

	assert.False(t, conn.Migrator().HasColumn(&UsageRecord{}, "cache_creation_input_tokens"))
	assert.False(t, conn.Migrator().HasColumn(&UsageRecord{}, "cache_read_input_tokens"))

	// Idempotent: a second run over the migrated schema changes nothing.
	require.NoError(t, ensureUsageRecordSchema(conn))
	require.NoError(t, conn.First(&got).Error)
	assert.Equal(t, 13, got.CacheReadTokens)
	assert.Equal(t, 7, got.CacheWriteTokens)
}

// TestEnsureUsageRecordSchema_MigratesOneLegacyColumn: a partially migrated
// database can carry one legacy column without the other. The previous SQL
// referenced both whenever either existed, so it would error out here.
func TestEnsureUsageRecordSchema_MigratesOneLegacyColumn(t *testing.T) {
	conn := openLegacyUsageDB(t)
	require.NoError(t, conn.Migrator().DropColumn(&UsageRecord{}, "cache_creation_input_tokens"))

	require.NoError(t, conn.Exec(`
		INSERT INTO usage_records
			(provider_uuid, provider_name, model, scenario, user_id, timestamp,
			 input_tokens, output_tokens, total_tokens, cache_read_input_tokens, status)
		VALUES ('p1','p1','m1','openai','u1',?,10,20,30,13,'success')`,
		time.Now()).Error)

	require.NoError(t, conn.AutoMigrate(&UsageRecord{}))
	require.NoError(t, ensureUsageRecordSchema(conn))

	var got UsageRecord
	require.NoError(t, conn.First(&got).Error)
	assert.Equal(t, 13, got.CacheReadTokens)
	assert.Equal(t, 0, got.CacheWriteTokens)
}

// TestEnsureUsageRecordSchema_BackfillsEmptyUserID covers the other half of
// the alignment step.
func TestEnsureUsageRecordSchema_BackfillsEmptyUserID(t *testing.T) {
	conn := openLegacyUsageDB(t)

	require.NoError(t, conn.Exec(`
		INSERT INTO usage_records
			(provider_uuid, provider_name, model, scenario, user_id, timestamp,
			 input_tokens, output_tokens, total_tokens, status)
		VALUES ('p1','p1','m1','openai','',?,10,20,30,'success')`, time.Now()).Error)

	require.NoError(t, conn.AutoMigrate(&UsageRecord{}))
	require.NoError(t, ensureUsageRecordSchema(conn))

	var got UsageRecord
	require.NoError(t, conn.First(&got).Error)
	assert.Equal(t, DefaultAdminUserID, got.UserID)
}

// TestPrepareUsageRecord_StampsAdminUser: new rows carry the default admin
// user, so the backfill above stays a one-shot legacy migration rather than
// rewriting every row written since the last restart.
func TestPrepareUsageRecord_StampsAdminUser(t *testing.T) {
	record := &UsageRecord{ProviderUUID: "p1", ProviderName: "p1", Model: "m1", Scenario: "openai"}
	prepareUsageRecord(record)
	assert.Equal(t, DefaultAdminUserID, record.UserID)

	explicit := &UsageRecord{UserID: "someone", ProviderUUID: "p1", ProviderName: "p1", Model: "m1", Scenario: "openai"}
	prepareUsageRecord(explicit)
	assert.Equal(t, "someone", explicit.UserID, "an authenticated user must not be overwritten")
}
