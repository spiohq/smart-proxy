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

	"github.com/klauspost/compress/zstd"
	"github.com/spiohq/smart-proxy/internal/blob"
)

// Reader retrieves previously-written body entries. The active hour lives on
// the local filesystem (currentDir); older tiers (recent/, archive/) are
// served from the configured backend.
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
// file is the filename portion only (e.g. "2026-04-20-17.jsonl" or
// "2026-04-20-17.jsonl.zst"); the reader probes current/, recent/, and
// archive/ until it finds a match. offset and length are byte coordinates
// into the uncompressed JSONL stream.
func (r *Reader) Read(ctx context.Context, file string, offset int64, length int) (*BodyEntry, error) {
	if filepath.Base(file) != file {
		return nil, fmt.Errorf("invalid body file name: %s", file)
	}

	// 1. current/ (always local, always uncompressed)
	localPath := filepath.Join(r.currentDir, file)
	if _, err := os.Stat(localPath); err == nil {
		return readDirect(localPath, offset, length)
	}

	// 2. recent/ (backend, uncompressed)
	if !strings.HasSuffix(file, ".zst") {
		key := "recent/" + file
		if entry, err := readFromBackendRange(ctx, r.backend, key, offset, length); err == nil {
			return entry, nil
		} else if !errors.Is(err, blob.ErrNotFound) {
			return nil, err
		}
	}

	// 3. archive/ (backend, zstd-compressed). Accept both bare and .zst
	// suffixed inputs to ease callers that don't know which tier they
	// stored into.
	archiveKey := "archive/" + file
	if !strings.HasSuffix(archiveKey, ".zst") {
		archiveKey += ".zst"
	}
	entry, err := readFromBackendCompressed(ctx, r.backend, archiveKey, offset, length)
	if err == nil {
		return entry, nil
	}
	if errors.Is(err, blob.ErrNotFound) {
		return nil, fmt.Errorf("body file not found: %s", file)
	}
	return nil, err
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

func readFromBackendCompressed(ctx context.Context, b blob.Backend, key string, offset int64, length int) (*BodyEntry, error) {
	rc, err := b.Get(ctx, key, 0, 0)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	dec, err := zstd.NewReader(rc)
	if err != nil {
		return nil, fmt.Errorf("zstd reader %s: %w", key, err)
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
