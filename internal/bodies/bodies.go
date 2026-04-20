package bodies

import (
	"context"
	"encoding/json"
)

// BodyStore handles request/response body storage on the filesystem.
type BodyStore interface {
	// Write appends a body entry to the current JSONL file.
	// Returns the file name, byte offset, and length for the DB reference.
	Write(ctx context.Context, entry *BodyEntry) (file string, offset int64, length int, err error)

	// Read retrieves a body entry by file + offset + length.
	// Hot/warm: <1ms (pread). Cold: 1-3s (decompress + seek).
	Read(ctx context.Context, file string, offset int64, length int) (*BodyEntry, error)

	// Close closes open file handles.
	Close() error
}

// BodyEntry is the JSONL structure written to the filesystem.
type BodyEntry struct {
	ID           string          `json:"id"`
	RequestBody  json.RawMessage `json:"req,omitempty"`
	ResponseBody json.RawMessage `json:"res,omitempty"`
}
