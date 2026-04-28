package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStore implements Store using SQLite via modernc.org/sqlite (pure Go).
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens a SQLite database and runs migrations.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	// Enable WAL mode for concurrent read/write
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}

	// Tighten file modes. SQLite creates the WAL and SHM files with the
	// process umask (typically 0o644 on Unix), not the main DB file's mode,
	// so we have to chmod each one explicitly. Missing files are tolerated
	// for the first-run case where the WAL has not yet been flushed.
	// Maintain() uses PRAGMA wal_checkpoint(TRUNCATE), which zeroes the WAL
	// in place rather than recreating the inode, so this chmod persists
	// for the lifetime of the process.
	if path != ":memory:" {
		for _, p := range []string{path, path + "-wal", path + "-shm"} {
			if err := os.Chmod(p, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
				db.Close()
				return nil, fmt.Errorf("chmod %s: %w", p, err)
			}
		}
	}

	store := &SQLiteStore{db: db}
	if err := store.Migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}

	return store, nil
}

// DB returns the underlying database connection for use by other stores
// sharing the same SQLite database.
func (s *SQLiteStore) DB() *sql.DB {
	return s.db
}

func (s *SQLiteStore) Migrate(ctx context.Context) error {
	return RunMigrations(ctx, s.db)
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) LogRequest(ctx context.Context, entry *RequestLog) error {
	return s.LogRequestBatch(ctx, []*RequestLog{entry})
}

func (s *SQLiteStore) LogRequestBatch(ctx context.Context, entries []*RequestLog) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO request_logs (
		id, timestamp, merchant_key, region, method, path, query_params,
		status_code,
		request_content_length, response_content_length,
		cache_status, queued, queue_wait_ms,
		upstream_latency_ms, total_latency_ms,
		pii_redacted, amazon_request_id,
		body_file, body_offset, body_length,
		cached_from_id, error_reason
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	for _, e := range entries {
		_, err := stmt.ExecContext(ctx,
			e.ID, e.Timestamp.UTC(), e.MerchantKey, e.Region, e.Method, e.Path, e.QueryParams,
			e.StatusCode,
			e.RequestContentLength, e.ResponseContentLength,
			e.CacheStatus, e.Queued, e.QueueWaitMs,
			e.UpstreamLatencyMs, e.TotalLatencyMs,
			e.PIIRedacted, e.AmazonRequestID,
			e.BodyFile, e.BodyOffset, e.BodyLength,
			e.CachedFromID, e.ErrorReason,
		)
		if err != nil {
			return fmt.Errorf("insert request log %s: %w", e.ID, err)
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) PurgeOlderThan(ctx context.Context, age time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-age)
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM request_logs WHERE timestamp < ?", cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("purge: %w", err)
	}
	return result.RowsAffected()
}

// Maintain runs a WAL checkpoint (TRUNCATE mode, which empties the WAL) and
// an incremental vacuum to reclaim freed pages. Incremental vacuum requires
// auto_vacuum to be enabled; if it is not the statement is a no-op.
func (s *SQLiteStore) Maintain(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return fmt.Errorf("wal checkpoint: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, "PRAGMA incremental_vacuum"); err != nil {
		return fmt.Errorf("incremental vacuum: %w", err)
	}
	return nil
}

// NullifyBodyRefs clears body pointers for rows whose body_file matches any
// of the supplied filenames. The filter is built as a single IN (...) clause
// so it stays a single round-trip regardless of slice size.
func (s *SQLiteStore) NullifyBodyRefs(ctx context.Context, files []string) (int64, error) {
	if len(files) == 0 {
		return 0, nil
	}
	placeholders := strings.Repeat("?,", len(files))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, 0, len(files))
	for _, f := range files {
		args = append(args, f)
	}
	query := fmt.Sprintf(
		"UPDATE request_logs SET body_file = '', body_offset = 0, body_length = 0 WHERE body_file IN (%s)",
		placeholders,
	)
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("nullify body refs: %w", err)
	}
	return result.RowsAffected()
}

// QueryByTimeRange returns request logs within the given time range [from, to).
// Headers are not populated; callers that need them must fetch the JSONL body.
func (s *SQLiteStore) QueryByTimeRange(ctx context.Context, from, to time.Time) ([]*RequestLog, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, timestamp, merchant_key, region, method, path, query_params,
			status_code,
			request_content_length, response_content_length,
			cache_status, queued, queue_wait_ms,
			upstream_latency_ms, total_latency_ms,
			pii_redacted, amazon_request_id,
			body_file, body_offset, body_length,
			cached_from_id, error_reason
		 FROM request_logs WHERE timestamp >= ? AND timestamp < ?
		 ORDER BY timestamp`, from.UTC(), to.UTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("query by time range: %w", err)
	}
	defer rows.Close()

	var result []*RequestLog
	for rows.Next() {
		e := &RequestLog{}
		err := rows.Scan(
			&e.ID, &e.Timestamp, &e.MerchantKey, &e.Region, &e.Method, &e.Path, &e.QueryParams,
			&e.StatusCode,
			&e.RequestContentLength, &e.ResponseContentLength,
			&e.CacheStatus, &e.Queued, &e.QueueWaitMs,
			&e.UpstreamLatencyMs, &e.TotalLatencyMs,
			&e.PIIRedacted, &e.AmazonRequestID,
			&e.BodyFile, &e.BodyOffset, &e.BodyLength,
			&e.CachedFromID, &e.ErrorReason,
		)
		if err != nil {
			return nil, fmt.Errorf("scan request log: %w", err)
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

// QueryByID returns a single request log by ID. Returns nil, nil if not found.
// Headers are not populated; callers that need them must fetch the JSONL body.
func (s *SQLiteStore) QueryByID(ctx context.Context, id string) (*RequestLog, error) {
	e := &RequestLog{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, timestamp, merchant_key, region, method, path, query_params,
			status_code,
			request_content_length, response_content_length,
			cache_status, queued, queue_wait_ms,
			upstream_latency_ms, total_latency_ms,
			pii_redacted, amazon_request_id,
			body_file, body_offset, body_length,
			cached_from_id, error_reason
		 FROM request_logs WHERE id = ?`, id,
	).Scan(
		&e.ID, &e.Timestamp, &e.MerchantKey, &e.Region, &e.Method, &e.Path, &e.QueryParams,
		&e.StatusCode,
		&e.RequestContentLength, &e.ResponseContentLength,
		&e.CacheStatus, &e.Queued, &e.QueueWaitMs,
		&e.UpstreamLatencyMs, &e.TotalLatencyMs,
		&e.PIIRedacted, &e.AmazonRequestID,
		&e.BodyFile, &e.BodyOffset, &e.BodyLength,
		&e.CachedFromID, &e.ErrorReason,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query by id: %w", err)
	}
	return e, nil
}

// QueryLogs searches request logs with flexible filtering and pagination.
func (s *SQLiteStore) QueryLogs(ctx context.Context, filter LogFilter) ([]*RequestLog, int64, error) {
	where, args := buildLogWhereClause(filter)

	// Count query
	var total int64
	countSQL := "SELECT COUNT(*) FROM request_logs" + where
	if err := s.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("query logs count: %w", err)
	}

	// Data query
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	dataSQL := `SELECT id, timestamp, merchant_key, region, method, path, query_params,
		status_code,
		request_content_length, response_content_length,
		cache_status, queued, queue_wait_ms,
		upstream_latency_ms, total_latency_ms,
		pii_redacted, amazon_request_id,
		body_file, body_offset, body_length,
		cached_from_id, error_reason
	 FROM request_logs` + where + ` ORDER BY timestamp DESC LIMIT ? OFFSET ?`

	dataArgs := append(args, limit, filter.Offset) //nolint:gocritic
	rows, err := s.db.QueryContext(ctx, dataSQL, dataArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query logs: %w", err)
	}
	defer rows.Close()

	var result []*RequestLog
	for rows.Next() {
		e := &RequestLog{}
		err := rows.Scan(
			&e.ID, &e.Timestamp, &e.MerchantKey, &e.Region, &e.Method, &e.Path, &e.QueryParams,
			&e.StatusCode,
			&e.RequestContentLength, &e.ResponseContentLength,
			&e.CacheStatus, &e.Queued, &e.QueueWaitMs,
			&e.UpstreamLatencyMs, &e.TotalLatencyMs,
			&e.PIIRedacted, &e.AmazonRequestID,
			&e.BodyFile, &e.BodyOffset, &e.BodyLength,
			&e.CachedFromID, &e.ErrorReason,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan log: %w", err)
		}
		result = append(result, e)
	}
	return result, total, rows.Err()
}

func buildLogWhereClause(f LogFilter) (string, []any) {
	var clauses []string
	var args []any

	if !f.From.IsZero() {
		clauses = append(clauses, "timestamp >= ?")
		args = append(args, f.From.UTC())
	}
	if !f.To.IsZero() {
		clauses = append(clauses, "timestamp < ?")
		args = append(args, f.To.UTC())
	}
	if f.Merchant != "" {
		clauses = append(clauses, "merchant_key LIKE ?")
		args = append(args, f.Merchant+"%")
	}
	if f.Region != "" {
		clauses = append(clauses, "region = ?")
		args = append(args, f.Region)
	}
	if f.Endpoint != "" {
		clauses = append(clauses, "path LIKE ?")
		args = append(args, f.Endpoint+"%")
	}
	if f.Status != "" {
		if len(f.Status) == 3 && f.Status[1:] == "xx" {
			// Bucket match: "4xx" -> status_code between 400 and 499
			digit := f.Status[0] - '0'
			clauses = append(clauses, "status_code >= ? AND status_code < ?")
			args = append(args, int(digit)*100, int(digit)*100+100)
		} else {
			clauses = append(clauses, "status_code = ?")
			args = append(args, f.Status)
		}
	}
	if f.CacheStatus != "" {
		clauses = append(clauses, "cache_status = ?")
		args = append(args, f.CacheStatus)
	}
	if f.Method != "" {
		clauses = append(clauses, "method = ?")
		args = append(args, f.Method)
	}
	if f.Queued == "true" {
		clauses = append(clauses, "queued = 1")
	} else if f.Queued == "false" {
		clauses = append(clauses, "queued = 0")
	}
	if f.MinLatency > 0 {
		clauses = append(clauses, "total_latency_ms >= ?")
		args = append(args, f.MinLatency)
	}
	if f.MaxLatency > 0 {
		clauses = append(clauses, "total_latency_ms <= ?")
		args = append(args, f.MaxLatency)
	}

	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// DistinctMerchants returns merchant keys matching the given prefix, ordered by most recent activity.
func (s *SQLiteStore) DistinctMerchants(ctx context.Context, prefix string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 20
	}
	var query string
	var args []any
	if prefix != "" {
		query = `SELECT merchant_key FROM request_logs
			WHERE merchant_key LIKE ? AND merchant_key != ''
			GROUP BY merchant_key ORDER BY MAX(timestamp) DESC LIMIT ?`
		args = []any{prefix + "%", limit}
	} else {
		query = `SELECT merchant_key FROM request_logs
			WHERE merchant_key != ''
			GROUP BY merchant_key ORDER BY MAX(timestamp) DESC LIMIT ?`
		args = []any{limit}
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("distinct merchants: %w", err)
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		result = append(result, key)
	}
	return result, rows.Err()
}

// MethodsByEndpoint returns the distinct HTTP methods seen per raw path within [from, to).
// The caller is responsible for classifying paths into endpoint patterns.
func (s *SQLiteStore) MethodsByEndpoint(ctx context.Context, from, to time.Time) (map[string][]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT method, path FROM request_logs
		 WHERE timestamp >= ? AND timestamp < ?`,
		from.UTC(), to.UTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("methods by endpoint: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]string)
	seen := make(map[string]map[string]bool) // path -> set of methods
	for rows.Next() {
		var method, path string
		if err := rows.Scan(&method, &path); err != nil {
			return nil, err
		}
		if seen[path] == nil {
			seen[path] = make(map[string]bool)
		}
		if !seen[path][method] {
			seen[path][method] = true
			result[path] = append(result[path], method)
		}
	}
	return result, rows.Err()
}
