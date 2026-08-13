package db

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// openLegacyUsageDB builds a usage_records table in its pre-split shape: the
// real current schema with the two cache columns renamed back to
// cache_read_input_tokens / cache_creation_input_tokens. Going through
// AutoMigrate and then renaming (rather than hand-writing DDL) keeps the
// table byte-for-byte GORM-shaped, which is what a real legacy database is.
func openLegacyUsageDB(t *testing.T) *gorm.DB {
	t.Helper()

	conn, err := OpenSQLite(filepath.Join(t.TempDir(), "legacy.db"), 0)
	require.NoError(t, err)
	require.NoError(t, conn.AutoMigrate(&UsageRecord{}))

	require.NoError(t, conn.Exec(
		`ALTER TABLE usage_records RENAME COLUMN cache_input_tokens TO cache_read_input_tokens`).Error)
	require.NoError(t, conn.Exec(
		`ALTER TABLE usage_records RENAME COLUMN cache_write_tokens TO cache_creation_input_tokens`).Error)

	return conn
}

// TestMigrateUsageTables_SplitsLegacyCacheColumns pins the mapping of the
// destructive cache-column migration: reads and writes must each land in
// their own column. An earlier version summed both into the read column and
// then dropped the sources, silently rebooking cache writes as cache reads.
func TestMigrateUsageTables_SplitsLegacyCacheColumns(t *testing.T) {
	conn := openLegacyUsageDB(t)

	require.NoError(t, conn.Exec(`
		INSERT INTO usage_records
			(provider_uuid, provider_name, model, scenario, user_id, timestamp,
			 input_tokens, output_tokens, total_tokens,
			 cache_creation_input_tokens, cache_read_input_tokens, status)
		VALUES ('p1','p1','m1','openai','u1',?,10,20,30,7,13,'success')`,
		time.Now()).Error)

	require.NoError(t, migrateUsageTables(conn))

	var got UsageRecord
	require.NoError(t, conn.First(&got).Error)
	assert.Equal(t, 13, got.CacheReadTokens, "cache_read_input_tokens must land in the read column")
	assert.Equal(t, 7, got.CacheWriteTokens, "cache_creation_input_tokens must land in the write column")

	assert.False(t, conn.Migrator().HasColumn(&UsageRecord{}, "cache_creation_input_tokens"))
	assert.False(t, conn.Migrator().HasColumn(&UsageRecord{}, "cache_read_input_tokens"))

	// Idempotent: a second run over the migrated schema changes nothing.
	require.NoError(t, migrateUsageTables(conn))
	require.NoError(t, conn.First(&got).Error)
	assert.Equal(t, 13, got.CacheReadTokens)
	assert.Equal(t, 7, got.CacheWriteTokens)
}

// TestMigrateUsageTables_MigratesOneLegacyColumn: a partially migrated DB can
// carry one legacy column without the other. The previous version's SQL
// referenced both whenever either existed, so it would error out here.
func TestMigrateUsageTables_MigratesOneLegacyColumn(t *testing.T) {
	conn := openLegacyUsageDB(t)
	require.NoError(t, conn.Migrator().DropColumn(&UsageRecord{}, "cache_creation_input_tokens"))

	require.NoError(t, conn.Exec(`
		INSERT INTO usage_records
			(provider_uuid, provider_name, model, scenario, user_id, timestamp,
			 input_tokens, output_tokens, total_tokens, cache_read_input_tokens, status)
		VALUES ('p1','p1','m1','openai','u1',?,10,20,30,13,'success')`,
		time.Now()).Error)

	require.NoError(t, migrateUsageTables(conn))

	var got UsageRecord
	require.NoError(t, conn.First(&got).Error)
	assert.Equal(t, 13, got.CacheReadTokens)
	assert.Equal(t, 0, got.CacheWriteTokens)
}

// TestMigrateUsageTables_BackfillsEmptyUserID covers the other half of
// ensureUsageRecordSchema, which also only ever ran on the test path before.
func TestMigrateUsageTables_BackfillsEmptyUserID(t *testing.T) {
	conn := openLegacyUsageDB(t)

	require.NoError(t, conn.Exec(`
		INSERT INTO usage_records
			(provider_uuid, provider_name, model, scenario, user_id, timestamp,
			 input_tokens, output_tokens, total_tokens, status)
		VALUES ('p1','p1','m1','openai','',?,10,20,30,'success')`, time.Now()).Error)

	require.NoError(t, migrateUsageTables(conn))

	var got UsageRecord
	require.NoError(t, conn.First(&got).Error)
	assert.Equal(t, DefaultAdminUserID, got.UserID)
}
