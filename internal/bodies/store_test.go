package bodies

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStore_WriteAndRead_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	entry := &BodyEntry{
		ID:           "req-roundtrip",
		RequestBody:  json.RawMessage(`{"input":"hello"}`),
		ResponseBody: json.RawMessage(`{"output":"world"}`),
	}

	file, offset, length, err := store.Write(ctx, entry)
	require.NoError(t, err)

	result, err := store.Read(ctx, file, offset, length)
	require.NoError(t, err)
	assert.Equal(t, "req-roundtrip", result.ID)
	assert.Equal(t, `{"input":"hello"}`, string(result.RequestBody))
	assert.Equal(t, `{"output":"world"}`, string(result.ResponseBody))
}

func TestStore_MultipleWriteAndRead(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Write 3 entries
	entries := []BodyEntry{
		{ID: "req-1", ResponseBody: json.RawMessage(`{"n":1}`)},
		{ID: "req-2", ResponseBody: json.RawMessage(`{"n":2}`)},
		{ID: "req-3", ResponseBody: json.RawMessage(`{"n":3}`)},
	}

	type ref struct {
		file   string
		offset int64
		length int
	}
	refs := make([]ref, len(entries))

	for i := range entries {
		f, o, l, err := store.Write(ctx, &entries[i])
		require.NoError(t, err)
		refs[i] = ref{f, o, l}
	}

	// Read them back in reverse order
	for i := len(refs) - 1; i >= 0; i-- {
		result, err := store.Read(ctx, refs[i].file, refs[i].offset, refs[i].length)
		require.NoError(t, err)
		assert.Equal(t, entries[i].ID, result.ID)
	}
}
