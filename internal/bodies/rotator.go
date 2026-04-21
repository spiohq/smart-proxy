package bodies

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spiohq/smart-proxy/internal/blob"
)

// OrphanNotifier is invoked by the rotator with the set of body filenames
// (without tier prefix or codec extension) it has just deleted. Implementors
// typically null out body_file pointers in metadata so dashboard queries do
// not surface dead references. Called synchronously inside RunOnce so the
// metadata view stays consistent with the object store.
type OrphanNotifier interface {
	OnBodiesDeleted(ctx context.Context, files []string) error
}

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
	codec         Codec
	currentDir    string
	recentMaxAge  time.Duration
	archiveMaxAge time.Duration
	maxBytes      int64
	notifier      OrphanNotifier
}

// RotatorOptions configures NewRotator. Fields left at their zero value
// receive sensible defaults (zstd codec).
type RotatorOptions struct {
	Codec         Codec
	RecentMaxAge  time.Duration
	ArchiveMaxAge time.Duration
	// MaxBytes is the hard cap on total body-storage bytes across current/,
	// recent/, and archive/. Zero disables size-based eviction. When the
	// total exceeds MaxBytes the rotator deletes oldest-first (by ModTime)
	// until the total fits.
	MaxBytes int64
	// OrphanNotifier (optional) is invoked with the filenames the rotator
	// just deleted so callers can null dangling metadata references.
	OrphanNotifier OrphanNotifier
}

// NewRotator creates a body rotator.
//   - backend: object store for recent/ and archive/ keys.
//   - currentDir: local filesystem directory holding the active hourly file.
//   - opts: compression codec, retention windows, and size cap.
func NewRotator(backend blob.Backend, currentDir string, opts RotatorOptions) *Rotator {
	codec := opts.Codec
	if codec == nil {
		codec, _ = NewCodec("zstd")
	}
	return &Rotator{
		backend:       backend,
		codec:         codec,
		currentDir:    currentDir,
		recentMaxAge:  opts.RecentMaxAge,
		archiveMaxAge: opts.ArchiveMaxAge,
		maxBytes:      opts.MaxBytes,
		notifier:      opts.OrphanNotifier,
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
	if err := r.evictBySize(ctx); err != nil {
		slog.Error("rotator: size eviction failed", "error", err)
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
		archiveKey := "archive/" + name + r.codec.Extension()
		if err := r.compressObject(ctx, obj.Key, archiveKey); err != nil {
			return fmt.Errorf("compress %s: %w", obj.Key, err)
		}
		if err := r.backend.Delete(ctx, obj.Key); err != nil {
			return fmt.Errorf("delete recent %s: %w", obj.Key, err)
		}
	}
	return nil
}

// compressObject streams src through the configured codec and writes the
// result to dst on the same backend.
func (r *Rotator) compressObject(ctx context.Context, srcKey, dstKey string) error {
	src, err := r.backend.Get(ctx, srcKey, 0, 0)
	if err != nil {
		return fmt.Errorf("get %s: %w", srcKey, err)
	}
	defer src.Close()

	pr, pw := io.Pipe()
	enc, err := r.codec.NewWriter(pw)
	if err != nil {
		pw.Close()
		return fmt.Errorf("codec writer: %w", err)
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
	var files []string
	for _, obj := range objects {
		if obj.ModTime.Before(cutoff) {
			expired = append(expired, obj.Key)
			files = append(files, baseFileName(obj.Key))
		}
	}
	if len(expired) == 0 {
		return nil
	}
	if err := r.backend.Delete(ctx, expired...); err != nil {
		return fmt.Errorf("delete expired: %w", err)
	}
	r.notifyOrphans(ctx, files)
	return nil
}

// evictBySize enforces the global MaxBytes budget across all three tiers.
// Inventory is built from the local current/ directory plus the backend's
// recent/ and archive/ prefixes, sorted by ModTime ascending, and oldest
// files are deleted until the total fits. Active hour's file is exempt.
func (r *Rotator) evictBySize(ctx context.Context) error {
	if r.maxBytes <= 0 {
		return nil
	}
	inventory, total, err := r.sizeInventory(ctx)
	if err != nil {
		return err
	}
	if total <= r.maxBytes {
		return nil
	}
	activeName := hourlyFileName(time.Now().UTC())
	var deletedFiles []string
	for _, item := range inventory {
		if total <= r.maxBytes {
			break
		}
		if item.file == activeName {
			continue
		}
		if item.localPath != "" {
			if err := os.Remove(item.localPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("evict %s: %w", item.localPath, err)
			}
		} else {
			if err := r.backend.Delete(ctx, item.key); err != nil {
				return fmt.Errorf("evict %s: %w", item.key, err)
			}
		}
		total -= item.size
		deletedFiles = append(deletedFiles, item.file)
		slog.Info("rotator: evicted by size", "file", item.file, "size", item.size, "remaining", total, "budget", r.maxBytes)
	}
	r.notifyOrphans(ctx, deletedFiles)
	return nil
}

type inventoryItem struct {
	file      string // bare filename (e.g. "2026-04-21-10.jsonl")
	key       string // backend key (empty if localPath is set)
	localPath string // absolute path (empty if key is set)
	size      int64
	modTime   time.Time
}

// sizeInventory returns every body object, oldest first by ModTime, along
// with the summed size.
func (r *Rotator) sizeInventory(ctx context.Context) ([]inventoryItem, int64, error) {
	var items []inventoryItem
	var total int64

	entries, err := os.ReadDir(r.currentDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, 0, fmt.Errorf("read current dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, 0, fmt.Errorf("stat current/%s: %w", e.Name(), err)
		}
		items = append(items, inventoryItem{
			file:      e.Name(),
			localPath: filepath.Join(r.currentDir, e.Name()),
			size:      info.Size(),
			modTime:   info.ModTime(),
		})
		total += info.Size()
	}

	for _, prefix := range []string{"recent/", "archive/"} {
		objs, err := r.backend.List(ctx, prefix)
		if err != nil {
			return nil, 0, fmt.Errorf("list %s: %w", prefix, err)
		}
		for _, obj := range objs {
			items = append(items, inventoryItem{
				file:    baseFileName(obj.Key),
				key:     obj.Key,
				size:    obj.Size,
				modTime: obj.ModTime,
			})
			total += obj.Size
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].modTime.Before(items[j].modTime)
	})
	return items, total, nil
}

// baseFileName strips tier prefix and any codec extension from a key so the
// returned value matches the body_file stored in metadata.
func baseFileName(key string) string {
	name := key
	if i := strings.LastIndex(key, "/"); i >= 0 {
		name = key[i+1:]
	}
	switch {
	case strings.HasSuffix(name, ".zst"):
		name = strings.TrimSuffix(name, ".zst")
	case strings.HasSuffix(name, ".gz"):
		name = strings.TrimSuffix(name, ".gz")
	}
	return name
}

func (r *Rotator) notifyOrphans(ctx context.Context, files []string) {
	if r.notifier == nil || len(files) == 0 {
		return
	}
	if err := r.notifier.OnBodiesDeleted(ctx, files); err != nil {
		slog.Error("rotator: orphan notify failed", "error", err, "count", len(files))
	}
}
