package bodies

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Store combines Writer and Reader into a complete BodyStore implementation.
type Store struct {
	writer *Writer
	reader *Reader
}

// NewStore creates a new BodyStore with the given base path.
// Creates the directory structure (current/, recent/, archive/) if needed.
func NewStore(basePath string) (*Store, error) {
	for _, sub := range []string{"current", "recent", "archive"} {
		if err := os.MkdirAll(filepath.Join(basePath, sub), 0755); err != nil {
			return nil, fmt.Errorf("create %s dir: %w", sub, err)
		}
	}

	writer, err := NewWriter(basePath)
	if err != nil {
		return nil, err
	}

	return &Store{
		writer: writer,
		reader: NewReader(basePath),
	}, nil
}

func (s *Store) Write(ctx context.Context, entry *BodyEntry) (string, int64, int, error) {
	return s.writer.Write(ctx, entry)
}

func (s *Store) Read(ctx context.Context, file string, offset int64, length int) (*BodyEntry, error) {
	return s.reader.Read(ctx, file, offset, length)
}

func (s *Store) Close() error {
	return s.writer.Close()
}
