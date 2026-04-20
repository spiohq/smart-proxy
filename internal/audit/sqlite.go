package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SQLiteStore implements Store using SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates an audit store using the given database connection.
func NewSQLiteStore(db *sql.DB) *SQLiteStore {
	return &SQLiteStore{db: db}
}

// LogAuditEvent inserts a single audit event.
func (s *SQLiteStore) LogAuditEvent(ctx context.Context, event *AuditEvent) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log (id, timestamp, event_type, source, message, metadata)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		event.ID, event.Timestamp.UTC(), event.EventType, event.Source, event.Message,
		MarshalMetadata(event.Metadata),
	)
	if err != nil {
		return fmt.Errorf("audit log: %w", err)
	}
	return nil
}

// QueryAuditEvents searches audit events with filtering and pagination.
func (s *SQLiteStore) QueryAuditEvents(ctx context.Context, filter AuditFilter) ([]*AuditEvent, int64, error) {
	var clauses []string
	var args []any

	if !filter.From.IsZero() {
		clauses = append(clauses, "timestamp >= ?")
		args = append(args, filter.From.UTC())
	}
	if !filter.To.IsZero() {
		clauses = append(clauses, "timestamp < ?")
		args = append(args, filter.To.UTC())
	}
	if filter.EventType != "" {
		clauses = append(clauses, "event_type = ?")
		args = append(args, filter.EventType)
	}

	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}

	// Count
	var total int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_log"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("audit query count: %w", err)
	}

	// Data
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	dataSQL := `SELECT id, timestamp, event_type, source, message, metadata
	 FROM audit_log` + where + ` ORDER BY timestamp DESC LIMIT ? OFFSET ?`
	dataArgs := append([]any{}, args...)
	dataArgs = append(dataArgs, limit, filter.Offset)

	rows, err := s.db.QueryContext(ctx, dataSQL, dataArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("audit query: %w", err)
	}
	defer rows.Close()

	var result []*AuditEvent
	for rows.Next() {
		e := &AuditEvent{}
		var metadata sql.NullString
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.EventType, &e.Source, &e.Message, &metadata); err != nil {
			return nil, 0, fmt.Errorf("audit scan: %w", err)
		}
		if metadata.Valid && metadata.String != "" {
			json.Unmarshal([]byte(metadata.String), &e.Metadata)
		}
		result = append(result, e)
	}
	return result, total, rows.Err()
}

// PurgeOlderThan deletes audit entries older than the given age.
func (s *SQLiteStore) PurgeOlderThan(ctx context.Context, age time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-age)
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM audit_log WHERE timestamp < ?", cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("audit purge: %w", err)
	}
	return result.RowsAffected()
}

// Close is a no-op  -  the DB connection is shared and closed by the storage layer.
func (s *SQLiteStore) Close() error {
	return nil
}
