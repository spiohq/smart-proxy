package bodies

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// Rotator moves body files through temperature tiers:
// current/ → recent/ (completed hours) → archive/ (zstd compressed) → purge.
type Rotator struct {
	basePath      string
	recentMaxAge  time.Duration
	archiveMaxAge time.Duration
}

// NewRotator creates a new body rotator.
func NewRotator(basePath string, recentMaxAge, archiveMaxAge time.Duration) *Rotator {
	return &Rotator{
		basePath:      basePath,
		recentMaxAge:  recentMaxAge,
		archiveMaxAge: archiveMaxAge,
	}
}

// RunOnce performs a single rotation cycle.
func (r *Rotator) RunOnce(ctx context.Context) error {
	if err := r.promoteCurrentToRecent(); err != nil {
		return fmt.Errorf("promote current to recent: %w", err)
	}
	if err := r.compressToArchive(); err != nil {
		return fmt.Errorf("compress to archive: %w", err)
	}
	if err := r.purgeExpired(); err != nil {
		return fmt.Errorf("purge expired: %w", err)
	}
	return nil
}

// Run starts the background rotation loop.
func (r *Rotator) Run(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.RunOnce(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// promoteCurrentToRecent moves completed hourly files from current/ to recent/.
func (r *Rotator) promoteCurrentToRecent() error {
	currentDir := filepath.Join(r.basePath, "current")
	recentDir := filepath.Join(r.basePath, "recent")

	if err := os.MkdirAll(recentDir, 0755); err != nil {
		return err
	}

	entries, err := os.ReadDir(currentDir)
	if err != nil {
		return err
	}

	currentFileName := hourlyFileName(time.Now().UTC())
	for _, e := range entries {
		if e.IsDir() || e.Name() == currentFileName {
			continue
		}
		src := filepath.Join(currentDir, e.Name())
		dst := filepath.Join(recentDir, e.Name())
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("move %s to recent: %w", e.Name(), err)
		}
	}
	return nil
}

// compressToArchive compresses recent/ files older than recentMaxAge to archive/.
func (r *Rotator) compressToArchive() error {
	recentDir := filepath.Join(r.basePath, "recent")
	archiveDir := filepath.Join(r.basePath, "archive")

	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		return err
	}

	entries, err := os.ReadDir(recentDir)
	if err != nil {
		return err
	}

	cutoff := time.Now().Add(-r.recentMaxAge)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}

		src := filepath.Join(recentDir, e.Name())
		dstName := strings.TrimSuffix(e.Name(), ".jsonl") + ".jsonl.zst"
		dst := filepath.Join(archiveDir, dstName)

		if err := compressFile(src, dst); err != nil {
			return fmt.Errorf("compress %s: %w", e.Name(), err)
		}

		if err := os.Remove(src); err != nil {
			return fmt.Errorf("remove compressed source %s: %w", e.Name(), err)
		}
	}
	return nil
}

// purgeExpired deletes archive files older than archiveMaxAge.
func (r *Rotator) purgeExpired() error {
	archiveDir := filepath.Join(r.basePath, "archive")

	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	cutoff := time.Now().Add(-r.archiveMaxAge)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(archiveDir, e.Name()))
		}
	}
	return nil
}

// compressFile reads src and writes a zstd-compressed version to dst.
func compressFile(src, dst string) error {
	input, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	enc, err := zstd.NewWriter(out)
	if err != nil {
		return err
	}

	if _, err := enc.Write(input); err != nil {
		enc.Close()
		return err
	}

	return enc.Close()
}
