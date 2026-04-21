package bodies

import (
	"context"
	"path/filepath"

	"github.com/spiohq/smart-proxy/internal/blob"
)

// Store combines Writer and Reader into a BodyStore implementation backed by
// a local "current/" directory for active writes and a blob.Backend for the
// recent/ and archive/ tiers.
type Store struct {
	writer  *Writer
	reader  *Reader
	backend blob.Backend
}

// NewStore creates a BodyStore. currentDir receives active writes; backend
// receives promoted/compressed files under the recent/ and archive/ key
// prefixes.
func NewStore(backend blob.Backend, currentDir string) (*Store, error) {
	w, err := NewWriter(currentDir)
	if err != nil {
		return nil, err
	}
	return &Store{
		writer:  w,
		reader:  NewReader(backend, currentDir),
		backend: backend,
	}, nil
}

// NewLocalStore is a convenience constructor that creates a local-backed
// store under basePath (basePath/current/, basePath/recent/, basePath/archive/).
func NewLocalStore(basePath string) (*Store, error) {
	backend, err := blob.NewLocal(basePath)
	if err != nil {
		return nil, err
	}
	return NewStore(backend, filepath.Join(basePath, "current"))
}

func (s *Store) Write(ctx context.Context, entry *BodyEntry) (string, int64, int, error) {
	return s.writer.Write(ctx, entry)
}

func (s *Store) Read(ctx context.Context, file string, offset int64, length int) (*BodyEntry, error) {
	return s.reader.Read(ctx, file, offset, length)
}

func (s *Store) Close() error {
	if err := s.writer.Close(); err != nil {
		return err
	}
	return s.backend.Close()
}
