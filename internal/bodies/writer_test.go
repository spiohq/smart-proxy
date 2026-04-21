package bodies

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupWriterTest(t *testing.T) (string, *Writer) {
	t.Helper()
	dir := t.TempDir()
	currentDir := filepath.Join(dir, "current")
	w, err := NewWriter(currentDir)
	require.NoError(t, err)
	t.Cleanup(func() { w.Close() })
	return currentDir, w
}

func TestWriter_Write_ReturnsFileAndOffset(t *testing.T) {
	_, w := setupWriterTest(t)
	ctx := context.Background()

	entry := &BodyEntry{
		ID:           "req-001",
		ResponseBody: json.RawMessage(`{"payload":"test"}`),
	}

	file, offset, length, err := w.Write(ctx, entry)
	require.NoError(t, err)
	assert.NotEmpty(t, file)
	assert.Equal(t, int64(0), offset)
	assert.Greater(t, length, 0)
}

func TestWriter_Write_SequentialOffsets(t *testing.T) {
	_, w := setupWriterTest(t)
	ctx := context.Background()

	entry1 := &BodyEntry{ID: "req-001", ResponseBody: json.RawMessage(`{"a":"1"}`)}
	entry2 := &BodyEntry{ID: "req-002", ResponseBody: json.RawMessage(`{"b":"2"}`)}

	file1, offset1, length1, err := w.Write(ctx, entry1)
	require.NoError(t, err)

	file2, offset2, _, err := w.Write(ctx, entry2)
	require.NoError(t, err)

	assert.Equal(t, file1, file2, "same hour = same file")
	assert.Equal(t, int64(0), offset1)
	assert.Equal(t, int64(length1), offset2, "second offset = first offset + first length")
}

func TestWriter_Write_ValidJSONL(t *testing.T) {
	currentDir, w := setupWriterTest(t)
	ctx := context.Background()

	entry := &BodyEntry{
		ID:           "req-001",
		RequestBody:  json.RawMessage(`{"input":"data"}`),
		ResponseBody: json.RawMessage(`{"output":"result"}`),
	}

	file, _, _, err := w.Write(ctx, entry)
	require.NoError(t, err)
	w.Close()

	content, err := os.ReadFile(filepath.Join(currentDir, file))
	require.NoError(t, err)

	var parsed BodyEntry
	require.NoError(t, json.Unmarshal(content[:len(content)-1], &parsed))
	assert.Equal(t, "req-001", parsed.ID)
	assert.Equal(t, `{"input":"data"}`, string(parsed.RequestBody))
	assert.Equal(t, `{"output":"result"}`, string(parsed.ResponseBody))
}

func TestWriter_Write_FileNameFormat(t *testing.T) {
	_, w := setupWriterTest(t)
	ctx := context.Background()

	entry := &BodyEntry{ID: "req-001", ResponseBody: json.RawMessage(`{}`)}
	file, _, _, err := w.Write(ctx, entry)
	require.NoError(t, err)

	assert.Regexp(t, `^\d{4}-\d{2}-\d{2}-\d{2}\.jsonl$`, file)
}
