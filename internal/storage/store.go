package storage

import (
	"context"
	"fmt"
	"time"
)

// Store is the interface for request log metadata storage.
type Store interface {
	// LogRequest writes a single request log entry.
	LogRequest(ctx context.Context, entry *RequestLog) error

	// LogRequestBatch writes multiple request log entries in a single transaction.
	LogRequestBatch(ctx context.Context, entries []*RequestLog) error

	// PurgeOlderThan deletes request logs older than the given age.
	// Returns the number of rows deleted.
	PurgeOlderThan(ctx context.Context, age time.Duration) (int64, error)

	// QueryByTimeRange returns request logs within the given time range [from, to).
	QueryByTimeRange(ctx context.Context, from, to time.Time) ([]*RequestLog, error)

	// QueryLogs searches request logs with flexible filtering.
	// Returns matching rows and total count for pagination.
	QueryLogs(ctx context.Context, filter LogFilter) ([]*RequestLog, int64, error)

	// QueryByID returns a single request log by ID. Returns nil, nil if not found.
	QueryByID(ctx context.Context, id string) (*RequestLog, error)

	// DistinctMerchants returns merchant keys matching the given prefix, ordered by most recent activity.
	DistinctMerchants(ctx context.Context, prefix string, limit int) ([]string, error)

	// MethodsByEndpoint returns the distinct HTTP methods used for each classified endpoint path
	// within the given time range. The returned map keys are endpoint paths (as stored in request_logs.path),
	// and values are slices of HTTP methods (e.g., ["GET", "POST"]).
	MethodsByEndpoint(ctx context.Context, from, to time.Time) (map[string][]string, error)

	// Migrate runs pending database migrations.
	Migrate(ctx context.Context) error

	// Close closes the database connection.
	Close() error
}

// RequestLog holds metadata for a single proxied request.
type RequestLog struct {
	ID                    string
	Timestamp             time.Time
	MerchantKey           string
	Region                string
	Method                string
	Path                  string
	QueryParams           string
	RequestHeaders        map[string]string // Redacted by caller
	StatusCode            int
	ResponseHeaders       map[string]string
	CacheStatus           string // HIT, MISS, BYPASS, PII_EXCLUDED
	Queued                bool
	QueueWaitMs           int64
	UpstreamLatencyMs     int64
	TotalLatencyMs        int64
	PIIRedacted           bool
	AmazonRequestID       string
	RequestContentLength  int64
	ResponseContentLength int64

	// Body reference (points to filesystem JSONL file)
	BodyFile   string // e.g., "2026-03-25-14.jsonl"
	BodyOffset int64  // Byte offset within the file
	BodyLength int    // Byte length of the JSONL entry

	// Error reason (set for 502 responses)
	ErrorReason string // e.g., "upstream_timeout", "connection_refused", "dns_resolution_failed"

	// Cache hit reference
	CachedFromID string // For cache hits: ID of the original request that populated the cache
}

// LogFilter defines criteria for querying request logs.
type LogFilter struct {
	From        time.Time
	To          time.Time
	Merchant    string // optional, prefix match
	Region      string // optional
	Endpoint    string // optional, SQL LIKE prefix match
	Status      string // optional: exact code "200" or bucket "4xx"
	CacheStatus string // optional: "HIT", "MISS", "BYPASS", "PII_EXCLUDED"
	Method      string // optional: "GET", "POST", etc.
	Queued      string // optional: "true" or "false"
	MinLatency  int64  // optional, 0 = no filter
	MaxLatency  int64  // optional, 0 = no filter
	Limit       int    // default 50, max 200
	Offset      int    // default 0
}

// NewStore creates a Store implementation based on the backend name.
func NewStore(backend, sqlitePath string) (Store, error) {
	switch backend {
	case "sqlite":
		return NewSQLiteStore(sqlitePath)
	default:
		return nil, fmt.Errorf("unsupported storage backend: %s", backend)
	}
}
