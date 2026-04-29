package blob

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
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

func TestLocalBackend_PutWritesAt0o600(t *testing.T) {
	root := t.TempDir()
	b, err := NewLocal(root)
	require.NoError(t, err)

	err = b.Put(context.Background(), "test/key", strings.NewReader("hello"), 5)
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(root, "test", "key"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// ── Symlink-safe path resolution (F-24) ──────────────────────────────────

func TestLocalBackend_RejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	// Plant a symlink inside root that points outside.
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "escape")))

	b, err := NewLocal(root)
	require.NoError(t, err)

	// Get through the symlink: the resolved target lives outside the
	// canonicalized backend root, so pathFor must refuse.
	_, err = b.Get(context.Background(), "escape/anyfile", 0, 0)
	require.Error(t, err, "Get of a key that traverses an out-of-root symlink must fail")
	assert.Contains(t, err.Error(), "invalid key")

	// Same protection applies to Put -- Put would have written into the
	// outside directory, which is exactly the harm.
	err = b.Put(context.Background(), "escape/newfile", strings.NewReader("x"), 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid key")
}

func TestLocalBackend_AllowsSymlinkInsideRoot(t *testing.T) {
	// A symlink that targets another path INSIDE the backend root must
	// still work. Otherwise legitimate operator setups (e.g. /data/bodies
	// is a symlink to /var/lib/proxy/bodies that resolves to a path also
	// owned by the proxy) would break.
	root := t.TempDir()
	inside := filepath.Join(root, "real")
	require.NoError(t, os.MkdirAll(inside, 0o700))
	require.NoError(t, os.Symlink(inside, filepath.Join(root, "alias")))

	// Plant a real file under "real/" via the OS (NewLocal canonicalizes
	// root to the resolved path of root, so an alias inside the original
	// root is still inside the canonicalized root because both target the
	// same filesystem).
	require.NoError(t, os.WriteFile(filepath.Join(inside, "f"), []byte("hello"), 0o600))

	b, err := NewLocal(root)
	require.NoError(t, err)

	rc, err := b.Get(context.Background(), "alias/f", 0, 0)
	require.NoError(t, err, "in-root symlink must resolve cleanly")
	defer rc.Close()
	got := make([]byte, 5)
	n, err := rc.Read(got)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(got[:n]))
}
