package bodies

import (
	"compress/gzip"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
)

// Codec is a pluggable stream compressor used when promoting recent/
// objects into archive/ objects. The zero-overhead "none" codec is useful
// for testing and for operators who prefer to let an S3 bucket handle
// compression via server-side lifecycle transforms.
type Codec interface {
	// Name is the canonical identifier ("zstd", "gzip", "none").
	Name() string

	// Extension is the file-name suffix appended to archive keys
	// (".zst", ".gz", or "" for none).
	Extension() string

	// NewWriter wraps w in an encoder. The returned WriteCloser must be
	// closed to flush the final frame.
	NewWriter(w io.Writer) (io.WriteCloser, error)

	// NewReader wraps r in a decoder. The returned ReadCloser must be
	// closed by the caller.
	NewReader(r io.Reader) (io.ReadCloser, error)
}

// NewCodec returns the codec matching name. Known names: "zstd", "gzip",
// "none". Unknown names yield an error.
func NewCodec(name string) (Codec, error) {
	switch name {
	case "", "zstd":
		return zstdCodec{}, nil
	case "gzip":
		return gzipCodec{}, nil
	case "none":
		return noneCodec{}, nil
	default:
		return nil, fmt.Errorf("unknown compression codec %q (want zstd|gzip|none)", name)
	}
}

type zstdCodec struct{}

func (zstdCodec) Name() string      { return "zstd" }
func (zstdCodec) Extension() string { return ".zst" }
func (zstdCodec) NewWriter(w io.Writer) (io.WriteCloser, error) {
	return zstd.NewWriter(w)
}
func (zstdCodec) NewReader(r io.Reader) (io.ReadCloser, error) {
	dec, err := zstd.NewReader(r)
	if err != nil {
		return nil, err
	}
	return &zstdReadCloser{dec: dec}, nil
}

// zstdReadCloser adapts *zstd.Decoder (Close is a no-arg method returning
// nothing) to io.ReadCloser.
type zstdReadCloser struct {
	dec *zstd.Decoder
}

func (z *zstdReadCloser) Read(p []byte) (int, error) { return z.dec.Read(p) }
func (z *zstdReadCloser) Close() error {
	z.dec.Close()
	return nil
}

type gzipCodec struct{}

func (gzipCodec) Name() string      { return "gzip" }
func (gzipCodec) Extension() string { return ".gz" }
func (gzipCodec) NewWriter(w io.Writer) (io.WriteCloser, error) {
	return gzip.NewWriter(w), nil
}
func (gzipCodec) NewReader(r io.Reader) (io.ReadCloser, error) {
	return gzip.NewReader(r)
}

type noneCodec struct{}

func (noneCodec) Name() string      { return "none" }
func (noneCodec) Extension() string { return "" }
func (noneCodec) NewWriter(w io.Writer) (io.WriteCloser, error) {
	return nopWriteCloser{w}, nil
}
func (noneCodec) NewReader(r io.Reader) (io.ReadCloser, error) {
	return io.NopCloser(r), nil
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
