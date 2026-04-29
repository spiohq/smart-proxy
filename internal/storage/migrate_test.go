package storage

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestRunMigrations_AppliesInitialSchema(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	err := RunMigrations(ctx, db)
	require.NoError(t, err)

	// Verify request_logs table exists
	_, err = db.ExecContext(ctx, "SELECT id FROM request_logs LIMIT 1")
	assert.NoError(t, err)

	// Verify schema_migrations records the version
	var version int
	err = db.QueryRowContext(ctx, "SELECT version FROM schema_migrations WHERE version = 1").Scan(&version)
	assert.NoError(t, err)
	assert.Equal(t, 1, version)
}

func TestRunMigrations_Idempotent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, RunMigrations(ctx, db))
	require.NoError(t, RunMigrations(ctx, db))

	// Should still have exactly five migrations applied (one per migration file, no duplicates)
	var count int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 5, count, "should have applied all migration files")
}

func TestRunMigrations_CreatesIndexes(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	require.NoError(t, RunMigrations(ctx, db))

	// Verify indexes exist by querying sqlite_master
	rows, err := db.QueryContext(ctx,
		"SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='request_logs'")
	require.NoError(t, err)
	defer rows.Close()

	var indexes []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		indexes = append(indexes, name)
	}

	assert.Contains(t, indexes, "idx_logs_merchant_time")
	assert.Contains(t, indexes, "idx_logs_status")
	assert.Contains(t, indexes, "idx_logs_path")
	assert.Contains(t, indexes, "idx_logs_cache")
}
