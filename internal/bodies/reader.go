package bodies

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// Reader implements the read side of BodyStore.
type Reader struct {
	basePath string
}

// NewReader creates a new body reader.
func NewReader(basePath string) *Reader {
	return &Reader{basePath: basePath}
}

// Read retrieves a body entry by file + offset + length.
func (r *Reader) Read(ctx context.Context, file string, offset int64, length int) (*BodyEntry, error) {
	path, err := r.resolvePath(file)
	if err != nil {
		return nil, err
	}

	if strings.HasSuffix(path, ".zst") {
		return r.readCompressed(path, offset, length)
	}

	return r.readDirect(path, offset, length)
}

// readDirect reads from an uncompressed file using ReadAt (pread).
func (r *Reader) readDirect(path string, offset int64, length int) (*BodyEntry, error) {
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

// readCompressed reads from a zstd-compressed file.
func (r *Reader) readCompressed(path string, offset int64, length int) (*BodyEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open compressed file: %w", err)
	}
	defer f.Close()

	dec, err := zstd.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("create zstd reader: %w", err)
	}
	defer dec.Close()

	// Skip to offset
	if offset > 0 {
		if _, err := io.CopyN(io.Discard, dec, offset); err != nil {
			return nil, fmt.Errorf("seek to offset %d: %w", offset, err)
		}
	}

	buf := make([]byte, length)
	if _, err := io.ReadFull(dec, buf); err != nil {
		return nil, fmt.Errorf("read compressed data: %w", err)
	}

	var entry BodyEntry
	if err := json.Unmarshal(buf, &entry); err != nil {
		return nil, fmt.Errorf("unmarshal body entry: %w", err)
	}
	return &entry, nil
}

// resolvePath finds the file in current/ → recent/ → archive/ (with .zst).
func (r *Reader) resolvePath(file string) (string, error) {
	// Prevent path traversal  -  file must be a plain filename
	if filepath.Base(file) != file {
		return "", fmt.Errorf("invalid body file name: %s", file)
	}

	candidates := []string{
		filepath.Join(r.basePath, "current", file),
		filepath.Join(r.basePath, "recent", file),
	}

	// For archive, check both plain and .zst suffix
	if strings.HasSuffix(file, ".zst") {
		candidates = append(candidates, filepath.Join(r.basePath, "archive", file))
	} else {
		candidates = append(candidates,
			filepath.Join(r.basePath, "archive", file),
			filepath.Join(r.basePath, "archive", file+".zst"),
		)
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("body file not found: %s", file)
}
