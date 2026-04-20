package bodies

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReader_ReadFromCurrent(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"current", "recent", "archive"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, sub), 0755))
	}

	// Write a body file in current/
	entry := &BodyEntry{ID: "req-001", ResponseBody: json.RawMessage(`{"data":"test"}`)}
	line, _ := json.Marshal(entry)
	line = append(line, '\n')
	require.NoError(t, os.WriteFile(filepath.Join(dir, "current", "2026-03-25-14.jsonl"), line, 0644))

	reader := NewReader(dir)
	result, err := reader.Read(context.Background(), "2026-03-25-14.jsonl", 0, len(line))
	require.NoError(t, err)
	assert.Equal(t, "req-001", result.ID)
	assert.Equal(t, `{"data":"test"}`, string(result.ResponseBody))
}

func TestReader_ReadFromRecent(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"current", "recent", "archive"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, sub), 0755))
	}

	entry := &BodyEntry{ID: "req-002", ResponseBody: json.RawMessage(`{"old":"data"}`)}
	line, _ := json.Marshal(entry)
	line = append(line, '\n')
	require.NoError(t, os.WriteFile(filepath.Join(dir, "recent", "2026-03-24-10.jsonl"), line, 0644))

	reader := NewReader(dir)
	result, err := reader.Read(context.Background(), "2026-03-24-10.jsonl", 0, len(line))
	require.NoError(t, err)
	assert.Equal(t, "req-002", result.ID)
}

func TestReader_ReadFromArchive(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"current", "recent", "archive"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, sub), 0755))
	}

	// Create a zstd-compressed archive file
	entry := &BodyEntry{ID: "req-003", ResponseBody: json.RawMessage(`{"archived":"yes"}`)}
	line, _ := json.Marshal(entry)
	line = append(line, '\n')

	archivePath := filepath.Join(dir, "archive", "2026-03-20.jsonl.zst")
	f, err := os.Create(archivePath)
	require.NoError(t, err)
	enc, err := zstd.NewWriter(f)
	require.NoError(t, err)
	_, err = enc.Write(line)
	require.NoError(t, err)
	enc.Close()
	f.Close()

	reader := NewReader(dir)
	result, err := reader.Read(context.Background(), "2026-03-20.jsonl.zst", 0, len(line))
	require.NoError(t, err)
	assert.Equal(t, "req-003", result.ID)
}

func TestReader_ReadAtOffset(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"current", "recent", "archive"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, sub), 0755))
	}

	entry1 := &BodyEntry{ID: "req-001", ResponseBody: json.RawMessage(`{"first":"entry"}`)}
	entry2 := &BodyEntry{ID: "req-002", ResponseBody: json.RawMessage(`{"second":"entry"}`)}
	line1, _ := json.Marshal(entry1)
	line1 = append(line1, '\n')
	line2, _ := json.Marshal(entry2)
	line2 = append(line2, '\n')

	content := append(line1, line2...)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "current", "test.jsonl"), content, 0644))

	reader := NewReader(dir)

	// Read second entry using offset
	result, err := reader.Read(context.Background(), "test.jsonl", int64(len(line1)), len(line2))
	require.NoError(t, err)
	assert.Equal(t, "req-002", result.ID)
}

func TestReader_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"current", "recent", "archive"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, sub), 0755))
	}

	reader := NewReader(dir)
	_, err := reader.Read(context.Background(), "nonexistent.jsonl", 0, 100)
	assert.Error(t, err)
}
