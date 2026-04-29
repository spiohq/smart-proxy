package audit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"
)

// AuditEvent represents a single audit log entry.
type AuditEvent struct {
	ID        string         `json:"id"`
	Timestamp time.Time      `json:"timestamp"`
	EventType string         `json:"eventType"`
	Source    string         `json:"source"`
	Message   string         `json:"message"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// AuditFilter defines criteria for querying audit events.
type AuditFilter struct {
	From      time.Time
	To        time.Time
	EventType string // optional
	Limit     int    // default 50, max 200
	Offset    int    // default 0
}

// Store persists audit events.
type Store interface {
	LogAuditEvent(ctx context.Context, event *AuditEvent) error
	// QueryAuditEvents searches audit events with filtering and pagination.
	QueryAuditEvents(ctx context.Context, filter AuditFilter) ([]*AuditEvent, int64, error)
	PurgeOlderThan(ctx context.Context, age time.Duration) (int64, error)
	Close() error
}

// AuditLogger provides a convenient API for logging audit events.
type AuditLogger struct {
	store Store
}

// NewAuditLogger creates an audit logger. If store is nil, all Log calls are no-ops.
func NewAuditLogger(store Store) *AuditLogger {
	return &AuditLogger{store: store}
}

// Log records an audit event. Safe to call on a nil *AuditLogger.
func (a *AuditLogger) Log(ctx context.Context, eventType, source, message string, metadata map[string]any) error {
	if a == nil || a.store == nil {
		return nil
	}
	event := &AuditEvent{
		ID:        generateID(),
		Timestamp: time.Now().UTC(),
		EventType: eventType,
		Source:    source,
		Message:   message,
		Metadata:  metadata,
	}
	return a.store.LogAuditEvent(ctx, event)
}

// Store returns the underlying store (for purge jobs).
func (a *AuditLogger) Store() Store {
	if a == nil {
		return nil
	}
	return a.store
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// MarshalMetadata converts metadata to JSON string. Returns "" if nil or empty.
func MarshalMetadata(m map[string]any) string {
	if len(m) == 0 {
		return ""
	}
	data, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(data)
}
