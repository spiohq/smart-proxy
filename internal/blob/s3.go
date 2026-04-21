package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// S3API is the narrow subset of the S3 client that S3Backend depends on.
// Defined locally so tests can mock it without pulling in the full SDK.
// The real *s3.Client satisfies it implicitly.
type S3API interface {
	PutObject(ctx context.Context, in *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(ctx context.Context, in *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	DeleteObjects(ctx context.Context, in *s3.DeleteObjectsInput, opts ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
	ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, opts ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	HeadObject(ctx context.Context, in *s3.HeadObjectInput, opts ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
}

// S3Options configures an S3Backend.
//
// Endpoint is optional; leave empty for real AWS S3, or set to a URL like
// "https://minio.internal:9000" or an R2 endpoint. AccessKey/SecretKey are
// also optional; when blank the SDK's default credential chain is used
// (IAM role, env vars, shared config).
type S3Options struct {
	Bucket    string
	Region    string
	Endpoint  string
	AccessKey string
	SecretKey string
	// PathStyle forces path-style addressing. Required for MinIO and most
	// self-hosted S3 implementations; R2 and real AWS work with either.
	PathStyle bool
}

// S3Backend implements Backend against an S3-compatible object store.
type S3Backend struct {
	client S3API
	bucket string
}

// NewS3 constructs an S3Backend by loading AWS config and instantiating a
// real S3 client with the given options.
func NewS3(ctx context.Context, opts S3Options) (*S3Backend, error) {
	if opts.Bucket == "" {
		return nil, fmt.Errorf("s3 backend: bucket is required")
	}
	var loadOpts []func(*awsconfig.LoadOptions) error
	if opts.Region != "" {
		loadOpts = append(loadOpts, awsconfig.WithRegion(opts.Region))
	}
	if opts.AccessKey != "" {
		loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(opts.AccessKey, opts.SecretKey, ""),
		))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("s3 backend: load aws config: %w", err)
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if opts.Endpoint != "" {
			o.BaseEndpoint = aws.String(opts.Endpoint)
		}
		o.UsePathStyle = opts.PathStyle
	})
	return &S3Backend{client: client, bucket: opts.Bucket}, nil
}

// NewS3WithClient lets callers inject a pre-built S3 client (or a mock).
func NewS3WithClient(client S3API, bucket string) *S3Backend {
	return &S3Backend{client: client, bucket: bucket}
}

func (b *S3Backend) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	input := &s3.PutObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(key),
		Body:   r,
	}
	if size >= 0 {
		input.ContentLength = aws.Int64(size)
	}
	if _, err := b.client.PutObject(ctx, input); err != nil {
		return fmt.Errorf("s3 put %s: %w", key, err)
	}
	return nil
}

func (b *S3Backend) Get(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(key),
	}
	if offset > 0 || length > 0 {
		input.Range = aws.String(s3RangeHeader(offset, length))
	}
	out, err := b.client.GetObject(ctx, input)
	if err != nil {
		if s3ErrIsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("s3 get %s: %w", key, err)
	}
	return out.Body, nil
}

func (b *S3Backend) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	// S3 DeleteObjects caps at 1000 keys per request; batch accordingly.
	const batch = 1000
	for i := 0; i < len(keys); i += batch {
		end := i + batch
		if end > len(keys) {
			end = len(keys)
		}
		ids := make([]types.ObjectIdentifier, 0, end-i)
		for _, k := range keys[i:end] {
			ids = append(ids, types.ObjectIdentifier{Key: aws.String(k)})
		}
		out, err := b.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(b.bucket),
			Delete: &types.Delete{Objects: ids},
		})
		if err != nil {
			return fmt.Errorf("s3 delete batch: %w", err)
		}
		if len(out.Errors) > 0 {
			return fmt.Errorf("s3 delete reported %d object errors (first: %s %s)",
				len(out.Errors),
				aws.ToString(out.Errors[0].Key),
				aws.ToString(out.Errors[0].Message))
		}
	}
	return nil
}

func (b *S3Backend) List(ctx context.Context, prefix string) ([]Object, error) {
	var objects []Object
	var token *string
	for {
		out, err := b.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(b.bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("s3 list %s: %w", prefix, err)
		}
		for _, o := range out.Contents {
			obj := Object{Key: aws.ToString(o.Key)}
			if o.Size != nil {
				obj.Size = *o.Size
			}
			if o.LastModified != nil {
				obj.ModTime = *o.LastModified
			}
			objects = append(objects, obj)
		}
		if out.IsTruncated == nil || !*out.IsTruncated {
			break
		}
		token = out.NextContinuationToken
	}
	return objects, nil
}

func (b *S3Backend) Stat(ctx context.Context, key string) (Object, error) {
	out, err := b.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if s3ErrIsNotFound(err) {
			return Object{}, ErrNotFound
		}
		return Object{}, fmt.Errorf("s3 head %s: %w", key, err)
	}
	obj := Object{Key: key}
	if out.ContentLength != nil {
		obj.Size = *out.ContentLength
	}
	if out.LastModified != nil {
		obj.ModTime = *out.LastModified
	}
	return obj, nil
}

// Close is a no-op; the underlying HTTP client is managed by the SDK.
func (b *S3Backend) Close() error { return nil }

// s3RangeHeader formats an HTTP Range header for the given offset + length.
// length<=0 means "from offset to end of object".
func s3RangeHeader(offset, length int64) string {
	if length <= 0 {
		return "bytes=" + strconv.FormatInt(offset, 10) + "-"
	}
	return "bytes=" + strconv.FormatInt(offset, 10) + "-" + strconv.FormatInt(offset+length-1, 10)
}

// s3ErrIsNotFound reports whether err signals a missing key. Covers both
// typed S3 errors (NoSuchKey, NotFound) and the smithy-go APIError path.
func s3ErrIsNotFound(err error) bool {
	var nsk *types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	var nf *types.NotFound
	if errors.As(err, &nf) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound":
			return true
		}
	}
	return false
}

// assert interface satisfaction at compile time.
var _ Backend = (*S3Backend)(nil)
var _ S3API = (*s3.Client)(nil)
