package bodies

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupRotatorDirs(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"current", "recent", "archive"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, sub), 0755))
	}
	return dir
}

func writeTestJSONL(t *testing.T, path string) {
	t.Helper()
	entry := &BodyEntry{ID: "test", ResponseBody: json.RawMessage(`{"test":true}`)}
	line, _ := json.Marshal(entry)
	line = append(line, '\n')
	require.NoError(t, os.WriteFile(path, line, 0644))
}

func TestRotator_PromoteCurrentToRecent(t *testing.T) {
	dir := setupRotatorDirs(t)

	// Create a file for a previous hour in current/
	pastHour := time.Now().UTC().Add(-2 * time.Hour)
	pastName := fmt.Sprintf("%04d-%02d-%02d-%02d.jsonl",
		pastHour.Year(), pastHour.Month(), pastHour.Day(), pastHour.Hour())
	writeTestJSONL(t, filepath.Join(dir, "current", pastName))

	// Create a file for the current hour (should NOT be moved)
	currentName := hourlyFileName(time.Now().UTC())
	writeTestJSONL(t, filepath.Join(dir, "current", currentName))

	rot := NewRotator(dir, 72*time.Hour, 8760*time.Hour)
	require.NoError(t, rot.RunOnce(context.Background()))

	// Past hour file should be in recent/
	assert.FileExists(t, filepath.Join(dir, "recent", pastName))
	assert.NoFileExists(t, filepath.Join(dir, "current", pastName))

	// Current hour file should still be in current/
	assert.FileExists(t, filepath.Join(dir, "current", currentName))
}

func TestRotator_CompressToArchive(t *testing.T) {
	dir := setupRotatorDirs(t)

	// Create a file in recent/ that's older than recentMaxAge
	oldName := "2026-03-20-10.jsonl"
	oldPath := filepath.Join(dir, "recent", oldName)
	writeTestJSONL(t, oldPath)
	// Set modification time to 4 days ago
	oldTime := time.Now().Add(-96 * time.Hour)
	require.NoError(t, os.Chtimes(oldPath, oldTime, oldTime))

	rot := NewRotator(dir, 72*time.Hour, 8760*time.Hour)
	require.NoError(t, rot.RunOnce(context.Background()))

	// Original should be deleted
	assert.NoFileExists(t, oldPath)

	// Archive should contain a .zst file
	entries, err := os.ReadDir(filepath.Join(dir, "archive"))
	require.NoError(t, err)
	assert.NotEmpty(t, entries)

	// Verify the archive file can be read back
	reader := NewReader(dir)
	entry := &BodyEntry{ID: "test", ResponseBody: json.RawMessage(`{"test":true}`)}
	line, _ := json.Marshal(entry)
	line = append(line, '\n')

	result, err := reader.Read(context.Background(), entries[0].Name(), 0, len(line))
	require.NoError(t, err)
	assert.Equal(t, "test", result.ID)
}

func TestRotator_PurgeExpiredArchives(t *testing.T) {
	dir := setupRotatorDirs(t)

	// Create an old archive file
	archivePath := filepath.Join(dir, "archive", "2024-01-01.jsonl.zst")
	require.NoError(t, os.WriteFile(archivePath, []byte("compressed"), 0644))
	oldTime := time.Now().Add(-400 * 24 * time.Hour) // ~400 days ago
	require.NoError(t, os.Chtimes(archivePath, oldTime, oldTime))

	// Create a recent archive file (should NOT be deleted)
	recentArchive := filepath.Join(dir, "archive", "2026-03-20.jsonl.zst")
	require.NoError(t, os.WriteFile(recentArchive, []byte("compressed"), 0644))

	rot := NewRotator(dir, 72*time.Hour, 8760*time.Hour)
	require.NoError(t, rot.RunOnce(context.Background()))

	assert.NoFileExists(t, archivePath)
	assert.FileExists(t, recentArchive)
}
