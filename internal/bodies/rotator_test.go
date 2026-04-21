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
	return currentDir, backend, NewRotator(backend, currentDir, recentMaxAge, archiveMaxAge)
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
