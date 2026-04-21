package bodies

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/spiohq/smart-proxy/internal/blob"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestReader(t *testing.T) (string, blob.Backend, *Reader) {
	t.Helper()
	dir := t.TempDir()
	currentDir := filepath.Join(dir, "current")
	require.NoError(t, os.MkdirAll(currentDir, 0o755))
	backend, err := blob.NewLocal(dir)
	require.NoError(t, err)
	t.Cleanup(func() { backend.Close() })
	return currentDir, backend, NewReader(backend, currentDir)
}

func TestReader_ReadFromCurrent(t *testing.T) {
	currentDir, _, reader := newTestReader(t)

	entry := &BodyEntry{ID: "req-001", ResponseBody: json.RawMessage(`{"data":"test"}`)}
	line, _ := json.Marshal(entry)
	line = append(line, '\n')
	require.NoError(t, os.WriteFile(filepath.Join(currentDir, "2026-03-25-14.jsonl"), line, 0o644))

	result, err := reader.Read(context.Background(), "2026-03-25-14.jsonl", 0, len(line))
	require.NoError(t, err)
	assert.Equal(t, "req-001", result.ID)
	assert.Equal(t, `{"data":"test"}`, string(result.ResponseBody))
}

func TestReader_ReadFromRecent(t *testing.T) {
	_, backend, reader := newTestReader(t)

	entry := &BodyEntry{ID: "req-002", ResponseBody: json.RawMessage(`{"old":"data"}`)}
	line, _ := json.Marshal(entry)
	line = append(line, '\n')
	require.NoError(t, backend.Put(context.Background(), "recent/2026-03-24-10.jsonl", bytes.NewReader(line), int64(len(line))))

	result, err := reader.Read(context.Background(), "2026-03-24-10.jsonl", 0, len(line))
	require.NoError(t, err)
	assert.Equal(t, "req-002", result.ID)
}

func TestReader_ReadFromArchive(t *testing.T) {
	_, backend, reader := newTestReader(t)

	entry := &BodyEntry{ID: "req-003", ResponseBody: json.RawMessage(`{"archived":"yes"}`)}
	line, _ := json.Marshal(entry)
	line = append(line, '\n')

	var compressed bytes.Buffer
	enc, err := zstd.NewWriter(&compressed)
	require.NoError(t, err)
	_, err = enc.Write(line)
	require.NoError(t, err)
	require.NoError(t, enc.Close())

	require.NoError(t, backend.Put(context.Background(), "archive/2026-03-20.jsonl.zst", &compressed, int64(compressed.Len())))

	result, err := reader.Read(context.Background(), "2026-03-20.jsonl.zst", 0, len(line))
	require.NoError(t, err)
	assert.Equal(t, "req-003", result.ID)
}

func TestReader_ReadFromArchive_BareFilename(t *testing.T) {
	// Callers that stored via body_file="2026-03-20.jsonl" should still find
	// the archive after it has been compressed and renamed with a .zst suffix.
	_, backend, reader := newTestReader(t)

	entry := &BodyEntry{ID: "req-004", ResponseBody: json.RawMessage(`{"bare":"name"}`)}
	line, _ := json.Marshal(entry)
	line = append(line, '\n')

	var compressed bytes.Buffer
	enc, err := zstd.NewWriter(&compressed)
	require.NoError(t, err)
	_, err = enc.Write(line)
	require.NoError(t, err)
	require.NoError(t, enc.Close())

	require.NoError(t, backend.Put(context.Background(), "archive/2026-03-20-10.jsonl.zst", &compressed, int64(compressed.Len())))

	result, err := reader.Read(context.Background(), "2026-03-20-10.jsonl", 0, len(line))
	require.NoError(t, err)
	assert.Equal(t, "req-004", result.ID)
}

func TestReader_ReadAtOffset(t *testing.T) {
	currentDir, _, reader := newTestReader(t)

	entry1 := &BodyEntry{ID: "req-001", ResponseBody: json.RawMessage(`{"first":"entry"}`)}
	entry2 := &BodyEntry{ID: "req-002", ResponseBody: json.RawMessage(`{"second":"entry"}`)}
	line1, _ := json.Marshal(entry1)
	line1 = append(line1, '\n')
	line2, _ := json.Marshal(entry2)
	line2 = append(line2, '\n')

	content := append(line1, line2...)
	require.NoError(t, os.WriteFile(filepath.Join(currentDir, "test.jsonl"), content, 0o644))

	result, err := reader.Read(context.Background(), "test.jsonl", int64(len(line1)), len(line2))
	require.NoError(t, err)
	assert.Equal(t, "req-002", result.ID)
}

func TestReader_FileNotFound(t *testing.T) {
	_, _, reader := newTestReader(t)

	_, err := reader.Read(context.Background(), "nonexistent.jsonl", 0, 100)
	assert.Error(t, err)
}
