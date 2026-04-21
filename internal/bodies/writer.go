package bodies

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Writer appends JSONL entries to the active hourly file in the local
// current/ directory. The active file always lives on local disk, regardless
// of the blob backend configured for older tiers, because the hot path
// cannot afford network round-trips.
type Writer struct {
	mu          sync.Mutex
	currentDir  string
	current     *os.File
	currentName string
	offset      int64
}

// NewWriter creates a writer that appends to currentDir. The directory is
// created if missing.
func NewWriter(currentDir string) (*Writer, error) {
	if err := os.MkdirAll(currentDir, 0o755); err != nil {
		return nil, fmt.Errorf("create current dir: %w", err)
	}
	return &Writer{currentDir: currentDir}, nil
}

// Write appends a body entry to the active hourly file.
func (w *Writer) Write(ctx context.Context, entry *BodyEntry) (string, int64, int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.rotateIfNeeded(); err != nil {
		return "", 0, 0, err
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return "", 0, 0, fmt.Errorf("marshal body entry: %w", err)
	}
	line = append(line, '\n')

	startOffset := w.offset
	n, err := w.current.Write(line)
	if err != nil {
		return "", 0, 0, fmt.Errorf("write body: %w", err)
	}
	w.offset += int64(n)
	return w.currentName, startOffset, n, nil
}

// Close closes the active file handle.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.current != nil {
		return w.current.Close()
	}
	return nil
}

func (w *Writer) rotateIfNeeded() error {
	name := hourlyFileName(time.Now().UTC())
	if name == w.currentName && w.current != nil {
		return nil
	}
	if w.current != nil {
		w.current.Close()
	}

	path := filepath.Join(w.currentDir, name)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open body file %s: %w", name, err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("stat body file: %w", err)
	}
	w.current = f
	w.currentName = name
	w.offset = info.Size()
	return nil
}

func hourlyFileName(t time.Time) string {
	return fmt.Sprintf("%04d-%02d-%02d-%02d.jsonl", t.Year(), t.Month(), t.Day(), t.Hour())
}
