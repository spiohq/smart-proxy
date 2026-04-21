package bodies

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/spiohq/smart-proxy/internal/blob"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRotator(t *testing.T, recentMaxAge, archiveMaxAge time.Duration) (string, blob.Backend, *Rotator) {
	t.Helper()
	dir := t.TempDir()
	currentDir := filepath.Join(dir, "current")
	require.NoError(t, os.MkdirAll(currentDir, 0o755))
	backend, err := blob.NewLocal(dir)
	require.NoError(t, err)
	t.Cleanup(func() { backend.Close() })
	return currentDir, backend, NewRotator(backend, currentDir, RotatorOptions{
		RecentMaxAge:  recentMaxAge,
		ArchiveMaxAge: archiveMaxAge,
	})
}

func writeTestJSONL(t *testing.T, path string) {
	t.Helper()
	entry := &BodyEntry{ID: "test", ResponseBody: json.RawMessage(`{"test":true}`)}
	line, _ := json.Marshal(entry)
	line = append(line, '\n')
	require.NoError(t, os.WriteFile(path, line, 0o644))
}

func TestRotator_PromoteCurrentToRecent(t *testing.T) {
	currentDir, backend, rot := newTestRotator(t, 72*time.Hour, 8760*time.Hour)

	pastHour := time.Now().UTC().Add(-2 * time.Hour)
	pastName := fmt.Sprintf("%04d-%02d-%02d-%02d.jsonl",
		pastHour.Year(), pastHour.Month(), pastHour.Day(), pastHour.Hour())
	writeTestJSONL(t, filepath.Join(currentDir, pastName))

	activeName := hourlyFileName(time.Now().UTC())
	writeTestJSONL(t, filepath.Join(currentDir, activeName))

	require.NoError(t, rot.RunOnce(context.Background()))

	// Past-hour file is now a recent/ object.
	_, err := backend.Stat(context.Background(), "recent/"+pastName)
	require.NoError(t, err)
	// And the local copy is gone.
	_, err = os.Stat(filepath.Join(currentDir, pastName))
	assert.True(t, os.IsNotExist(err))
	// Active-hour file is untouched.
	_, err = os.Stat(filepath.Join(currentDir, activeName))
	require.NoError(t, err)
}

func TestRotator_CompressToArchive(t *testing.T) {
	_, backend, rot := newTestRotator(t, 72*time.Hour, 8760*time.Hour)

	// Seed a recent/ object whose backing file is old enough to trigger compression.
	ctx := context.Background()
	entry := &BodyEntry{ID: "test", ResponseBody: json.RawMessage(`{"test":true}`)}
	line, _ := json.Marshal(entry)
	line = append(line, '\n')

	name := "2026-03-20-10.jsonl"
	require.NoError(t, backend.Put(ctx, "recent/"+name, bytes.NewReader(line), int64(len(line))))

	// Backdate the underlying file so it looks aged.
	local := backend.(*blob.LocalBackend)
	backingPath := filepath.Join(local.Root(), "recent", name)
	oldTime := time.Now().Add(-96 * time.Hour)
	require.NoError(t, os.Chtimes(backingPath, oldTime, oldTime))

	require.NoError(t, rot.RunOnce(ctx))

	// Recent/ source is gone.
	_, err := backend.Stat(ctx, "recent/"+name)
	assert.Error(t, err)

	// Archive/ object exists and round-trips through zstd to the original bytes.
	archiveKey := "archive/" + name + ".zst"
	stat, err := backend.Stat(ctx, archiveKey)
	require.NoError(t, err)
	assert.Greater(t, stat.Size, int64(0))

	rc, err := backend.Get(ctx, archiveKey, 0, 0)
	require.NoError(t, err)
	defer rc.Close()
	dec, err := zstd.NewReader(rc)
	require.NoError(t, err)
	defer dec.Close()
	decoded, err := io.ReadAll(dec)
	require.NoError(t, err)
	assert.Equal(t, line, decoded)
}

func TestRotator_CompressWithGzipCodec(t *testing.T) {
	dir := t.TempDir()
	currentDir := filepath.Join(dir, "current")
	require.NoError(t, os.MkdirAll(currentDir, 0o755))
	backend, err := blob.NewLocal(dir)
	require.NoError(t, err)
	defer backend.Close()

	gzCodec, err := NewCodec("gzip")
	require.NoError(t, err)
	rot := NewRotator(backend, currentDir, RotatorOptions{
		Codec:         gzCodec,
		RecentMaxAge:  72 * time.Hour,
		ArchiveMaxAge: 8760 * time.Hour,
	})

	ctx := context.Background()
	name := "2026-03-20-10.jsonl"
	entry := &BodyEntry{ID: "gz", ResponseBody: json.RawMessage(`{"x":1}`)}
	line, _ := json.Marshal(entry)
	line = append(line, '\n')

	require.NoError(t, backend.Put(ctx, "recent/"+name, bytes.NewReader(line), int64(len(line))))
	backingPath := filepath.Join(backend.Root(), "recent", name)
	oldTime := time.Now().Add(-96 * time.Hour)
	require.NoError(t, os.Chtimes(backingPath, oldTime, oldTime))

	require.NoError(t, rot.RunOnce(ctx))

	// Archive must use .gz extension, not .zst.
	_, err = backend.Stat(ctx, "archive/"+name+".gz")
	require.NoError(t, err)
	_, err = backend.Stat(ctx, "archive/"+name+".zst")
	assert.Error(t, err)

	// And the Reader can still read it via extension sniffing.
	reader := NewReader(backend, currentDir)
	result, err := reader.Read(ctx, name, 0, len(line))
	require.NoError(t, err)
	assert.Equal(t, "gz", result.ID)
}

func TestRotator_PurgeExpiredArchives(t *testing.T) {
	_, backend, rot := newTestRotator(t, 72*time.Hour, 8760*time.Hour)

	ctx := context.Background()
	require.NoError(t, backend.Put(ctx, "archive/2024-01-01.jsonl.zst", bytes.NewReader([]byte("compressed")), 10))
	require.NoError(t, backend.Put(ctx, "archive/2026-03-20.jsonl.zst", bytes.NewReader([]byte("compressed")), 10))

	local := backend.(*blob.LocalBackend)
	oldTime := time.Now().Add(-400 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(local.Root(), "archive", "2024-01-01.jsonl.zst"), oldTime, oldTime))

	require.NoError(t, rot.RunOnce(ctx))

	_, err := backend.Stat(ctx, "archive/2024-01-01.jsonl.zst")
	assert.Error(t, err)
	_, err = backend.Stat(ctx, "archive/2026-03-20.jsonl.zst")
	require.NoError(t, err)
}

// recordingNotifier captures files the rotator reports as deleted.
type recordingNotifier struct{ seen []string }

func (r *recordingNotifier) OnBodiesDeleted(_ context.Context, files []string) error {
	r.seen = append(r.seen, files...)
	return nil
}

func TestRotator_EvictBySize_DeletesOldestUntilUnderBudget(t *testing.T) {
	dir := t.TempDir()
	currentDir := filepath.Join(dir, "current")
	require.NoError(t, os.MkdirAll(currentDir, 0o755))
	backend, err := blob.NewLocal(dir)
	require.NoError(t, err)
	defer backend.Close()

	notifier := &recordingNotifier{}
	rot := NewRotator(backend, currentDir, RotatorOptions{
		RecentMaxAge:   72 * time.Hour,
		ArchiveMaxAge:  8760 * time.Hour,
		MaxBytes:       100,
		OrphanNotifier: notifier,
	})

	ctx := context.Background()
	// Three 60-byte archive objects, staggered mtimes: oldest first.
	payload := bytes.Repeat([]byte("x"), 60)
	require.NoError(t, backend.Put(ctx, "archive/2026-04-18-10.jsonl.zst", bytes.NewReader(payload), 60))
	require.NoError(t, backend.Put(ctx, "archive/2026-04-19-10.jsonl.zst", bytes.NewReader(payload), 60))
	require.NoError(t, backend.Put(ctx, "archive/2026-04-20-10.jsonl.zst", bytes.NewReader(payload), 60))
	now := time.Now()
	require.NoError(t, os.Chtimes(filepath.Join(backend.Root(), "archive", "2026-04-18-10.jsonl.zst"), now.Add(-3*time.Hour), now.Add(-3*time.Hour)))
	require.NoError(t, os.Chtimes(filepath.Join(backend.Root(), "archive", "2026-04-19-10.jsonl.zst"), now.Add(-2*time.Hour), now.Add(-2*time.Hour)))
	require.NoError(t, os.Chtimes(filepath.Join(backend.Root(), "archive", "2026-04-20-10.jsonl.zst"), now.Add(-1*time.Hour), now.Add(-1*time.Hour)))

	require.NoError(t, rot.RunOnce(ctx))

	// 180 bytes total, budget 100: must delete two oldest to land at 60.
	_, err = backend.Stat(ctx, "archive/2026-04-18-10.jsonl.zst")
	assert.Error(t, err)
	_, err = backend.Stat(ctx, "archive/2026-04-19-10.jsonl.zst")
	assert.Error(t, err)
	_, err = backend.Stat(ctx, "archive/2026-04-20-10.jsonl.zst")
	require.NoError(t, err)

	// Notifier should see the two evicted filenames (bare names, no extension).
	assert.ElementsMatch(t, []string{"2026-04-18-10.jsonl", "2026-04-19-10.jsonl"}, notifier.seen)
}

func TestRotator_EvictBySize_SkipsActiveHour(t *testing.T) {
	dir := t.TempDir()
	currentDir := filepath.Join(dir, "current")
	require.NoError(t, os.MkdirAll(currentDir, 0o755))
	backend, err := blob.NewLocal(dir)
	require.NoError(t, err)
	defer backend.Close()

	rot := NewRotator(backend, currentDir, RotatorOptions{
		RecentMaxAge:  72 * time.Hour,
		ArchiveMaxAge: 8760 * time.Hour,
		MaxBytes:      10,
	})

	ctx := context.Background()
	activeName := hourlyFileName(time.Now().UTC())
	activePath := filepath.Join(currentDir, activeName)
	require.NoError(t, os.WriteFile(activePath, bytes.Repeat([]byte("y"), 100), 0o644))

	require.NoError(t, rot.RunOnce(ctx))

	// Active hour must not be evicted even when way over budget.
	_, err = os.Stat(activePath)
	require.NoError(t, err)
}

func TestRotator_EvictBySize_DisabledWhenZero(t *testing.T) {
	dir := t.TempDir()
	currentDir := filepath.Join(dir, "current")
	require.NoError(t, os.MkdirAll(currentDir, 0o755))
	backend, err := blob.NewLocal(dir)
	require.NoError(t, err)
	defer backend.Close()

	rot := NewRotator(backend, currentDir, RotatorOptions{
		RecentMaxAge:  72 * time.Hour,
		ArchiveMaxAge: 8760 * time.Hour,
		MaxBytes:      0,
	})

	ctx := context.Background()
	payload := bytes.Repeat([]byte("z"), 10_000)
	require.NoError(t, backend.Put(ctx, "archive/2026-04-18-10.jsonl.zst", bytes.NewReader(payload), int64(len(payload))))

	require.NoError(t, rot.RunOnce(ctx))

	// MaxBytes=0 disables eviction entirely.
	_, err = backend.Stat(ctx, "archive/2026-04-18-10.jsonl.zst")
	require.NoError(t, err)
}

func TestRotator_PurgeExpired_NotifiesOrphans(t *testing.T) {
	dir := t.TempDir()
	currentDir := filepath.Join(dir, "current")
	require.NoError(t, os.MkdirAll(currentDir, 0o755))
	backend, err := blob.NewLocal(dir)
	require.NoError(t, err)
	defer backend.Close()

	notifier := &recordingNotifier{}
	rot := NewRotator(backend, currentDir, RotatorOptions{
		RecentMaxAge:   72 * time.Hour,
		ArchiveMaxAge:  8760 * time.Hour,
		OrphanNotifier: notifier,
	})

	ctx := context.Background()
	require.NoError(t, backend.Put(ctx, "archive/2024-01-01.jsonl.zst", bytes.NewReader([]byte("x")), 1))
	require.NoError(t, os.Chtimes(filepath.Join(backend.Root(), "archive", "2024-01-01.jsonl.zst"),
		time.Now().Add(-400*24*time.Hour), time.Now().Add(-400*24*time.Hour)))

	require.NoError(t, rot.RunOnce(ctx))

	assert.Equal(t, []string{"2024-01-01.jsonl"}, notifier.seen)
}
