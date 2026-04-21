package bodies

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/spiohq/smart-proxy/internal/blob"
)

// Rotator moves body files through temperature tiers:
// current/ (local) -> recent/ (backend) -> archive/ (backend, compressed)
// -> purge.
//
// The current hour is always written to the local filesystem beneath
// currentDir. Promotion uploads the closed hourly file to the backend under
// the "recent/" key prefix. Archive is another prefix on the same backend,
// so a local backend mirrors the previous directory layout exactly while an
// S3 backend ships both tiers off the node.
type Rotator struct {
	backend       blob.Backend
	currentDir    string
	recentMaxAge  time.Duration
	archiveMaxAge time.Duration
}

// NewRotator creates a body rotator.
//   - backend: object store for recent/ and archive/ keys.
//   - currentDir: local filesystem directory holding the active hourly file.
//   - recentMaxAge: age at which a recent/ object is compressed to archive/.
//   - archiveMaxAge: age at which an archive/ object is deleted.
func NewRotator(backend blob.Backend, currentDir string, recentMaxAge, archiveMaxAge time.Duration) *Rotator {
	return &Rotator{
		backend:       backend,
		currentDir:    currentDir,
		recentMaxAge:  recentMaxAge,
		archiveMaxAge: archiveMaxAge,
	}
}

// RunOnce performs a single rotation cycle. Errors from individual phases
// are logged but do not abort the remaining phases: a failed promotion
// should not prevent purge from reclaiming space.
func (r *Rotator) RunOnce(ctx context.Context) error {
	var firstErr error
	if err := r.promoteCurrentToRecent(ctx); err != nil {
		slog.Error("rotator: promote current->recent failed", "error", err)
		firstErr = err
	}
	if err := r.compressToArchive(ctx); err != nil {
		slog.Error("rotator: compress recent->archive failed", "error", err)
		if firstErr == nil {
			firstErr = err
		}
	}
	if err := r.purgeExpired(ctx); err != nil {
		slog.Error("rotator: purge archive failed", "error", err)
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Run starts the background rotation loop.
func (r *Rotator) Run(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = r.RunOnce(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// promoteCurrentToRecent uploads completed hourly files from the local
// current/ directory to the backend under recent/<name>, then deletes the
// local copy on successful upload.
func (r *Rotator) promoteCurrentToRecent(ctx context.Context) error {
	entries, err := os.ReadDir(r.currentDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read current dir: %w", err)
	}
	activeName := hourlyFileName(time.Now().UTC())
	for _, e := range entries {
		if e.IsDir() || e.Name() == activeName || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		src := filepath.Join(r.currentDir, e.Name())
		f, err := os.Open(src)
		if err != nil {
			return fmt.Errorf("open %s: %w", src, err)
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return fmt.Errorf("stat %s: %w", src, err)
		}
		key := "recent/" + e.Name()
		if err := r.backend.Put(ctx, key, f, info.Size()); err != nil {
			f.Close()
			return fmt.Errorf("put %s: %w", key, err)
		}
		f.Close()
		if err := os.Remove(src); err != nil {
			return fmt.Errorf("remove local %s: %w", src, err)
		}
	}
	return nil
}

// compressToArchive compresses recent/ objects older than recentMaxAge into
// archive/<name>.zst, then deletes the recent/ source.
func (r *Rotator) compressToArchive(ctx context.Context) error {
	objects, err := r.backend.List(ctx, "recent/")
	if err != nil {
		return fmt.Errorf("list recent: %w", err)
	}
	cutoff := time.Now().Add(-r.recentMaxAge)
	for _, obj := range objects {
		if !strings.HasSuffix(obj.Key, ".jsonl") {
			continue
		}
		if obj.ModTime.After(cutoff) {
			continue
		}
		name := strings.TrimPrefix(obj.Key, "recent/")
		archiveKey := "archive/" + name + ".zst"
		if err := r.compressObject(ctx, obj.Key, archiveKey); err != nil {
			return fmt.Errorf("compress %s: %w", obj.Key, err)
		}
		if err := r.backend.Delete(ctx, obj.Key); err != nil {
			return fmt.Errorf("delete recent %s: %w", obj.Key, err)
		}
	}
	return nil
}

// compressObject streams src through zstd and writes the result to dst on
// the same backend.
func (r *Rotator) compressObject(ctx context.Context, srcKey, dstKey string) error {
	src, err := r.backend.Get(ctx, srcKey, 0, 0)
	if err != nil {
		return fmt.Errorf("get %s: %w", srcKey, err)
	}
	defer src.Close()

	pr, pw := io.Pipe()
	enc, err := zstd.NewWriter(pw)
	if err != nil {
		pw.Close()
		return fmt.Errorf("zstd writer: %w", err)
	}
	errCh := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(enc, src)
		closeErr := enc.Close()
		if copyErr == nil {
			copyErr = closeErr
		}
		pw.CloseWithError(copyErr)
		errCh <- copyErr
	}()
	if err := r.backend.Put(ctx, dstKey, pr, -1); err != nil {
		pr.CloseWithError(err)
		<-errCh
		return fmt.Errorf("put %s: %w", dstKey, err)
	}
	if err := <-errCh; err != nil {
		return fmt.Errorf("encode %s: %w", srcKey, err)
	}
	return nil
}

// purgeExpired deletes archive/ objects older than archiveMaxAge.
func (r *Rotator) purgeExpired(ctx context.Context) error {
	objects, err := r.backend.List(ctx, "archive/")
	if err != nil {
		return fmt.Errorf("list archive: %w", err)
	}
	cutoff := time.Now().Add(-r.archiveMaxAge)
	var expired []string
	for _, obj := range objects {
		if obj.ModTime.Before(cutoff) {
			expired = append(expired, obj.Key)
		}
	}
	if len(expired) == 0 {
		return nil
	}
	if err := r.backend.Delete(ctx, expired...); err != nil {
		return fmt.Errorf("delete expired: %w", err)
	}
	return nil
}
