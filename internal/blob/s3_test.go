package blob

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeS3 is an in-memory S3API implementation sufficient for round-trip tests.
type fakeS3 struct {
	objects map[string]fakeObject
	// hook lets individual tests assert input shape.
	lastPutRange string
	lastPutSSE   types.ServerSideEncryption
	lastPutKMS   string
}

type fakeObject struct {
	body     []byte
	modified time.Time
}

func newFakeS3() *fakeS3 { return &fakeS3{objects: map[string]fakeObject{}} }

func (f *fakeS3) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	b, err := io.ReadAll(in.Body)
	if err != nil {
		return nil, err
	}
	f.objects[aws.ToString(in.Key)] = fakeObject{body: b, modified: time.Now().UTC()}
	f.lastPutSSE = in.ServerSideEncryption
	f.lastPutKMS = aws.ToString(in.SSEKMSKeyId)
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeS3) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	key := aws.ToString(in.Key)
	obj, ok := f.objects[key]
	if !ok {
		return nil, &types.NoSuchKey{Message: aws.String("not found")}
	}
	body := obj.body
	if r := aws.ToString(in.Range); r != "" {
		f.lastPutRange = r
		var start, end int64
		// Parse "bytes=start-end" or "bytes=start-"
		trimmed := strings.TrimPrefix(r, "bytes=")
		parts := strings.SplitN(trimmed, "-", 2)
		_, _ = parseInt(parts[0], &start)
		if len(parts) == 2 && parts[1] != "" {
			_, _ = parseInt(parts[1], &end)
			if end >= int64(len(body)) {
				end = int64(len(body)) - 1
			}
			body = body[start : end+1]
		} else {
			body = body[start:]
		}
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(body))}, nil
}

func parseInt(s string, dst *int64) (bool, error) {
	var v int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return false, nil
		}
		v = v*10 + int64(c-'0')
	}
	*dst = v
	return true, nil
}

func (f *fakeS3) DeleteObject(_ context.Context, in *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	delete(f.objects, aws.ToString(in.Key))
	return &s3.DeleteObjectOutput{}, nil
}

func (f *fakeS3) DeleteObjects(_ context.Context, in *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	for _, id := range in.Delete.Objects {
		delete(f.objects, aws.ToString(id.Key))
	}
	return &s3.DeleteObjectsOutput{}, nil
}

func (f *fakeS3) ListObjectsV2(_ context.Context, in *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	prefix := aws.ToString(in.Prefix)
	var contents []types.Object
	for k, obj := range f.objects {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		size := int64(len(obj.body))
		mod := obj.modified
		contents = append(contents, types.Object{
			Key:          aws.String(k),
			Size:         &size,
			LastModified: &mod,
		})
	}
	trunc := false
	return &s3.ListObjectsV2Output{Contents: contents, IsTruncated: &trunc}, nil
}

func (f *fakeS3) HeadObject(_ context.Context, in *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	obj, ok := f.objects[aws.ToString(in.Key)]
	if !ok {
		return nil, &smithy.GenericAPIError{Code: "NotFound", Message: "not found"}
	}
	size := int64(len(obj.body))
	mod := obj.modified
	return &s3.HeadObjectOutput{ContentLength: &size, LastModified: &mod}, nil
}

func TestS3Backend_PutGetRoundtrip(t *testing.T) {
	ctx := context.Background()
	fake := newFakeS3()
	b := NewS3WithClient(fake, "bucket")

	payload := []byte("hello world")
	require.NoError(t, b.Put(ctx, "recent/a.jsonl", bytes.NewReader(payload), int64(len(payload))))

	rc, err := b.Get(ctx, "recent/a.jsonl", 0, 0)
	require.NoError(t, err)
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	assert.Equal(t, payload, got)
}

func TestS3Backend_GetRange(t *testing.T) {
	ctx := context.Background()
	fake := newFakeS3()
	b := NewS3WithClient(fake, "bucket")

	payload := []byte("abcdefghij")
	require.NoError(t, b.Put(ctx, "k", bytes.NewReader(payload), int64(len(payload))))

	rc, err := b.Get(ctx, "k", 3, 4)
	require.NoError(t, err)
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	assert.Equal(t, []byte("defg"), got)
	assert.Equal(t, "bytes=3-6", fake.lastPutRange)
}

func TestS3Backend_GetNotFound(t *testing.T) {
	ctx := context.Background()
	fake := newFakeS3()
	b := NewS3WithClient(fake, "bucket")

	_, err := b.Get(ctx, "nope", 0, 0)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestS3Backend_List(t *testing.T) {
	ctx := context.Background()
	fake := newFakeS3()
	b := NewS3WithClient(fake, "bucket")

	require.NoError(t, b.Put(ctx, "archive/a.zst", bytes.NewReader([]byte("1")), 1))
	require.NoError(t, b.Put(ctx, "archive/b.zst", bytes.NewReader([]byte("22")), 2))
	require.NoError(t, b.Put(ctx, "recent/c.jsonl", bytes.NewReader([]byte("333")), 3))

	objs, err := b.List(ctx, "archive/")
	require.NoError(t, err)
	require.Len(t, objs, 2)
	keys := []string{objs[0].Key, objs[1].Key}
	assert.ElementsMatch(t, []string{"archive/a.zst", "archive/b.zst"}, keys)
}

func TestS3Backend_DeleteBatch(t *testing.T) {
	ctx := context.Background()
	fake := newFakeS3()
	b := NewS3WithClient(fake, "bucket")

	require.NoError(t, b.Put(ctx, "x", bytes.NewReader([]byte("1")), 1))
	require.NoError(t, b.Put(ctx, "y", bytes.NewReader([]byte("2")), 1))
	require.NoError(t, b.Delete(ctx, "x", "y"))

	_, err := b.Get(ctx, "x", 0, 0)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestS3Backend_StatNotFound(t *testing.T) {
	ctx := context.Background()
	fake := newFakeS3()
	b := NewS3WithClient(fake, "bucket")

	_, err := b.Stat(ctx, "missing")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestS3RangeHeader(t *testing.T) {
	assert.Equal(t, "bytes=0-9", s3RangeHeader(0, 10))
	assert.Equal(t, "bytes=100-", s3RangeHeader(100, 0))
	assert.Equal(t, "bytes=5-5", s3RangeHeader(5, 1))
}

func TestS3Backend_PutWithoutSSE(t *testing.T) {
	ctx := context.Background()
	fake := newFakeS3()
	b := NewS3WithClient(fake, "bucket")
	require.NoError(t, b.Put(ctx, "k", bytes.NewReader([]byte("x")), 1))
	assert.Equal(t, types.ServerSideEncryption(""), fake.lastPutSSE)
	assert.Equal(t, "", fake.lastPutKMS)
}

func TestS3Backend_PutWithAES256(t *testing.T) {
	ctx := context.Background()
	fake := newFakeS3()
	b, err := NewS3WithClientSSE(fake, "bucket", "AES256", "")
	require.NoError(t, err)
	require.NoError(t, b.Put(ctx, "k", bytes.NewReader([]byte("x")), 1))
	assert.Equal(t, types.ServerSideEncryptionAes256, fake.lastPutSSE)
	assert.Equal(t, "", fake.lastPutKMS)
}

func TestS3Backend_PutWithKMS(t *testing.T) {
	ctx := context.Background()
	fake := newFakeS3()
	b, err := NewS3WithClientSSE(fake, "bucket", "aws:kms", "alias/proxy-bodies")
	require.NoError(t, err)
	require.NoError(t, b.Put(ctx, "k", bytes.NewReader([]byte("x")), 1))
	assert.Equal(t, types.ServerSideEncryptionAwsKms, fake.lastPutSSE)
	assert.Equal(t, "alias/proxy-bodies", fake.lastPutKMS)
}

func TestS3Backend_PutWithKMSDsse(t *testing.T) {
	ctx := context.Background()
	fake := newFakeS3()
	b, err := NewS3WithClientSSE(fake, "bucket", "aws:kms:dsse", "")
	require.NoError(t, err)
	require.NoError(t, b.Put(ctx, "k", bytes.NewReader([]byte("x")), 1))
	assert.Equal(t, types.ServerSideEncryptionAwsKmsDsse, fake.lastPutSSE)
	// KMS key omitted when caller didn't supply one.
	assert.Equal(t, "", fake.lastPutKMS)
}

func TestS3Backend_KMSKeyIgnoredForAES256(t *testing.T) {
	ctx := context.Background()
	fake := newFakeS3()
	b, err := NewS3WithClientSSE(fake, "bucket", "AES256", "alias/ignored")
	require.NoError(t, err)
	require.NoError(t, b.Put(ctx, "k", bytes.NewReader([]byte("x")), 1))
	assert.Equal(t, types.ServerSideEncryptionAes256, fake.lastPutSSE)
	assert.Equal(t, "", fake.lastPutKMS, "KMS key must not leak into AES256 puts")
}

func TestS3Backend_InvalidSSE(t *testing.T) {
	_, err := NewS3WithClientSSE(newFakeS3(), "bucket", "rot13", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported SSE")
}
