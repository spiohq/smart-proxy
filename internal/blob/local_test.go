package blob

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocal_PutGetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	b, err := NewLocal(dir)
	require.NoError(t, err)
	defer b.Close()

	ctx := context.Background()
	payload := []byte("hello world\nwith multiple lines\n")
	require.NoError(t, b.Put(ctx, "recent/2026-04-20-17.jsonl", bytes.NewReader(payload), int64(len(payload))))

	r, err := b.Get(ctx, "recent/2026-04-20-17.jsonl", 0, 0)
	require.NoError(t, err)
	got, err := io.ReadAll(r)
	require.NoError(t, err)
	r.Close()
	assert.Equal(t, payload, got)
}

func TestLocal_GetRange(t *testing.T) {
	dir := t.TempDir()
	b, err := NewLocal(dir)
	require.NoError(t, err)
	defer b.Close()

	ctx := context.Background()
	payload := []byte("AAAABBBBCCCCDDDD")
	require.NoError(t, b.Put(ctx, "x.dat", bytes.NewReader(payload), int64(len(payload))))

	r, err := b.Get(ctx, "x.dat", 4, 8)
	require.NoError(t, err)
	got, err := io.ReadAll(r)
	require.NoError(t, err)
	r.Close()
	assert.Equal(t, []byte("BBBBCCCC"), got)
}

func TestLocal_StatMissing(t *testing.T) {
	dir := t.TempDir()
	b, err := NewLocal(dir)
	require.NoError(t, err)
	defer b.Close()

	_, err = b.Stat(context.Background(), "nope.jsonl")
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestLocal_GetMissing(t *testing.T) {
	dir := t.TempDir()
	b, err := NewLocal(dir)
	require.NoError(t, err)
	defer b.Close()

	_, err = b.Get(context.Background(), "nope.jsonl", 0, 0)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestLocal_ListByPrefix(t *testing.T) {
	dir := t.TempDir()
	b, err := NewLocal(dir)
	require.NoError(t, err)
	defer b.Close()

	ctx := context.Background()
	for _, k := range []string{
		"recent/a.jsonl",
		"recent/b.jsonl",
		"archive/c.jsonl.zst",
	} {
		require.NoError(t, b.Put(ctx, k, bytes.NewReader([]byte("x")), 1))
	}

	got, err := b.List(ctx, "recent/")
	require.NoError(t, err)
	keys := make([]string, 0, len(got))
	for _, o := range got {
		keys = append(keys, o.Key)
	}
	assert.ElementsMatch(t, []string{"recent/a.jsonl", "recent/b.jsonl"}, keys)
}

func TestLocal_Delete(t *testing.T) {
	dir := t.TempDir()
	b, err := NewLocal(dir)
	require.NoError(t, err)
	defer b.Close()

	ctx := context.Background()
	require.NoError(t, b.Put(ctx, "a.jsonl", bytes.NewReader([]byte("x")), 1))
	require.NoError(t, b.Put(ctx, "b.jsonl", bytes.NewReader([]byte("y")), 1))

	require.NoError(t, b.Delete(ctx, "a.jsonl", "missing.jsonl", "b.jsonl"))

	_, err = b.Stat(ctx, "a.jsonl")
	assert.True(t, errors.Is(err, ErrNotFound))
	_, err = b.Stat(ctx, "b.jsonl")
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestLocal_PutOverwrite(t *testing.T) {
	dir := t.TempDir()
	b, err := NewLocal(dir)
	require.NoError(t, err)
	defer b.Close()

	ctx := context.Background()
	require.NoError(t, b.Put(ctx, "k", bytes.NewReader([]byte("one")), 3))
	require.NoError(t, b.Put(ctx, "k", bytes.NewReader([]byte("two")), 3))

	r, err := b.Get(ctx, "k", 0, 0)
	require.NoError(t, err)
	got, err := io.ReadAll(r)
	require.NoError(t, err)
	r.Close()
	assert.Equal(t, []byte("two"), got)
}

func TestLocal_KeyValidation(t *testing.T) {
	dir := t.TempDir()
	b, err := NewLocal(dir)
	require.NoError(t, err)
	defer b.Close()

	ctx := context.Background()
	err = b.Put(ctx, "../escape", bytes.NewReader([]byte("x")), 1)
	assert.Error(t, err)

	err = b.Put(ctx, "", bytes.NewReader([]byte("x")), 1)
	assert.Error(t, err)
}
