package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalBackend implements Backend against a local filesystem directory.
// Keys map to files under root, with '/' in keys translated to the OS path
// separator. Intermediate directories are created on Put.
type LocalBackend struct {
	root string
}

// NewLocal creates a local filesystem backend rooted at root. The directory
// is created if missing.
func NewLocal(root string) (*LocalBackend, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create blob root %s: %w", root, err)
	}
	return &LocalBackend{root: root}, nil
}

func (b *LocalBackend) pathFor(key string) (string, error) {
	if key == "" || strings.Contains(key, "..") {
		return "", fmt.Errorf("invalid key %q", key)
	}
	return filepath.Join(b.root, filepath.FromSlash(key)), nil
}

func (b *LocalBackend) Put(ctx context.Context, key string, r io.Reader, _ int64) error {
	path, err := b.pathFor(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir for %s: %w", key, err)
	}
	// Atomic write: stage to tmp then rename.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".blob-*.tmp")
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	_ = tmp.Chmod(0o600)
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close tmp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename tmp to %s: %w", path, err)
	}
	return nil
}

func (b *LocalBackend) Get(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	path, err := b.pathFor(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("open %s: %w", key, err)
	}
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			f.Close()
			return nil, fmt.Errorf("seek %s to %d: %w", key, offset, err)
		}
	}
	if length > 0 {
		return &limitedCloser{Reader: io.LimitReader(f, length), closer: f}, nil
	}
	return f, nil
}

func (b *LocalBackend) Delete(ctx context.Context, keys ...string) error {
	for _, key := range keys {
		path, err := b.pathFor(key)
		if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("delete %s: %w", key, err)
		}
	}
	return nil
}

func (b *LocalBackend) List(ctx context.Context, prefix string) ([]Object, error) {
	var objects []Object
	// Translate the prefix to a filesystem path for walking. Prefixes that
	// do not end in '/' are still treated as filename prefixes; we walk the
	// parent directory and filter.
	prefixPath := filepath.Join(b.root, filepath.FromSlash(prefix))
	walkRoot := prefixPath
	if !strings.HasSuffix(prefix, "/") {
		walkRoot = filepath.Dir(prefixPath)
	}
	err := filepath.WalkDir(walkRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return filepath.SkipDir
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(b.root, p)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if !strings.HasPrefix(key, prefix) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		objects = append(objects, Object{
			Key:     key,
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("list %s: %w", prefix, err)
	}
	return objects, nil
}

func (b *LocalBackend) Stat(ctx context.Context, key string) (Object, error) {
	path, err := b.pathFor(key)
	if err != nil {
		return Object{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Object{}, ErrNotFound
		}
		return Object{}, fmt.Errorf("stat %s: %w", key, err)
	}
	return Object{
		Key:     key,
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}, nil
}

func (b *LocalBackend) Close() error { return nil }

// Root returns the absolute filesystem root. Used by callers that need a
// local path for the active write file (the hot path bypasses Put).
func (b *LocalBackend) Root() string { return b.root }

type limitedCloser struct {
	io.Reader
	closer io.Closer
}

func (l *limitedCloser) Close() error { return l.closer.Close() }
