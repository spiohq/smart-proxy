package bodies

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spiohq/smart-proxy/internal/blob"
)

// Reader retrieves previously-written body entries. The active hour lives on
// the local filesystem (currentDir); older tiers (recent/, archive/) are
// served from the configured backend. Archive objects are decompressed
// based on their filename suffix (.zst, .gz, or uncompressed).
type Reader struct {
	backend    blob.Backend
	currentDir string
}

// NewReader creates a body reader.
func NewReader(backend blob.Backend, currentDir string) *Reader {
	return &Reader{backend: backend, currentDir: currentDir}
}

// Read retrieves a body entry identified by (file, offset, length).
//
// file is the filename portion only (e.g. "2026-04-20-17.jsonl",
// "2026-04-20-17.jsonl.zst", or "2026-04-20-17.jsonl.gz"); the reader
// probes current/, recent/, and archive/ until it finds a match. offset
// and length are byte coordinates into the uncompressed JSONL stream.
func (r *Reader) Read(ctx context.Context, file string, offset int64, length int) (*BodyEntry, error) {
	if filepath.Base(file) != file {
		return nil, fmt.Errorf("invalid body file name: %s", file)
	}

	// 1. current/ (always local, always uncompressed).
	localPath := filepath.Join(r.currentDir, file)
	if _, err := os.Stat(localPath); err == nil {
		return readDirect(localPath, offset, length)
	}

	// 2. recent/ (backend, uncompressed).
	if codecFromSuffix(file) == nil {
		key := "recent/" + file
		if entry, err := readFromBackendRange(ctx, r.backend, key, offset, length); err == nil {
			return entry, nil
		} else if !errors.Is(err, blob.ErrNotFound) {
			return nil, err
		}
	}

	// 3. archive/ (backend). Try the explicit filename first, then any
	// known codec extension. This lets callers that stored
	// body_file="2026-04-20.jsonl" still resolve after promotion renamed
	// the object to "2026-04-20.jsonl.zst" or ".gz".
	candidates := []string{"archive/" + file}
	if codecFromSuffix(file) == nil {
		candidates = append(candidates, "archive/"+file+".zst", "archive/"+file+".gz")
	}
	for _, key := range candidates {
		codec := codecFromSuffix(key)
		var entry *BodyEntry
		var err error
		if codec == nil {
			entry, err = readFromBackendRange(ctx, r.backend, key, offset, length)
		} else {
			entry, err = readFromBackendCompressed(ctx, r.backend, key, codec, offset, length)
		}
		if err == nil {
			return entry, nil
		}
		if !errors.Is(err, blob.ErrNotFound) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("body file not found: %s", file)
}

// codecFromSuffix returns a decoder for the given key's extension, or nil
// if the key is uncompressed.
func codecFromSuffix(key string) Codec {
	switch {
	case strings.HasSuffix(key, ".zst"):
		c, _ := NewCodec("zstd")
		return c
	case strings.HasSuffix(key, ".gz"):
		c, _ := NewCodec("gzip")
		return c
	default:
		return nil
	}
}

func readDirect(path string, offset int64, length int) (*BodyEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open body file: %w", err)
	}
	defer f.Close()

	buf := make([]byte, length)
	if _, err := f.ReadAt(buf, offset); err != nil {
		return nil, fmt.Errorf("read at offset %d: %w", offset, err)
	}
	var entry BodyEntry
	if err := json.Unmarshal(buf, &entry); err != nil {
		return nil, fmt.Errorf("unmarshal body entry: %w", err)
	}
	return &entry, nil
}

func readFromBackendRange(ctx context.Context, b blob.Backend, key string, offset int64, length int) (*BodyEntry, error) {
	rc, err := b.Get(ctx, key, offset, int64(length))
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	buf := make([]byte, length)
	if _, err := io.ReadFull(rc, buf); err != nil {
		return nil, fmt.Errorf("read %s: %w", key, err)
	}
	var entry BodyEntry
	if err := json.Unmarshal(buf, &entry); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", key, err)
	}
	return &entry, nil
}

func readFromBackendCompressed(ctx context.Context, b blob.Backend, key string, codec Codec, offset int64, length int) (*BodyEntry, error) {
	rc, err := b.Get(ctx, key, 0, 0)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	dec, err := codec.NewReader(rc)
	if err != nil {
		return nil, fmt.Errorf("%s reader %s: %w", codec.Name(), key, err)
	}
	defer dec.Close()

	if offset > 0 {
		if _, err := io.CopyN(io.Discard, dec, offset); err != nil {
			return nil, fmt.Errorf("skip to offset %d in %s: %w", offset, key, err)
		}
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(dec, buf); err != nil {
		return nil, fmt.Errorf("read %s: %w", key, err)
	}
	var entry BodyEntry
	if err := json.Unmarshal(buf, &entry); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", key, err)
	}
	return &entry, nil
}
