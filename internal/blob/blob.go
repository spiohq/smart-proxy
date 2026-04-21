// Package blob defines a minimal object-storage abstraction used by the body
// storage pipeline. Implementations back promoted and archived body files;
// active writes (current hour) always go to the local filesystem regardless
// of backend, since write latency on the hot path cannot tolerate network
// round-trips.
package blob

import (
	"context"
	"io"
	"time"
)

// Backend is an object store addressed by flat keys.
//
// Keys are slash-separated logical paths, e.g. "recent/2026-04-20-17.jsonl"
// or "archive/2026-04-20-17.jsonl.zst". Backends must accept any valid UTF-8
// key and must not impose directory semantics beyond what is needed to
// satisfy List(prefix).
type Backend interface {
	// Put stores the data read from r under key. size is the expected number
	// of bytes; backends may use it for pre-allocation or multipart sizing.
	// If size is unknown, callers pass -1.
	Put(ctx context.Context, key string, r io.Reader, size int64) error

	// Get returns a reader for the byte range [offset, offset+length) within
	// the object at key. If length <= 0, the reader returns the object from
	// offset to end. The caller must Close() the returned reader.
	Get(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error)

	// Delete removes one or more keys. Missing keys are not an error.
	Delete(ctx context.Context, keys ...string) error

	// List returns metadata for all objects whose key starts with prefix.
	// Order is unspecified.
	List(ctx context.Context, prefix string) ([]Object, error)

	// Stat returns metadata for a single object. Returns ErrNotFound if the
	// key does not exist.
	Stat(ctx context.Context, key string) (Object, error)

	// Close releases any backend resources (pool, client, open files).
	Close() error
}

// Object is the metadata returned by List and Stat.
type Object struct {
	Key     string
	Size    int64
	ModTime time.Time
}
