package bodies

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodecs_RoundTrip(t *testing.T) {
	payload := []byte("hello world, this is a repeated payload. hello world, this is a repeated payload.")

	for _, name := range []string{"zstd", "gzip", "none"} {
		t.Run(name, func(t *testing.T) {
			codec, err := NewCodec(name)
			require.NoError(t, err)
			assert.Equal(t, name, codec.Name())

			var buf bytes.Buffer
			w, err := codec.NewWriter(&buf)
			require.NoError(t, err)
			_, err = w.Write(payload)
			require.NoError(t, err)
			require.NoError(t, w.Close())

			r, err := codec.NewReader(&buf)
			require.NoError(t, err)
			got, err := io.ReadAll(r)
			require.NoError(t, err)
			require.NoError(t, r.Close())
			assert.Equal(t, payload, got)
		})
	}
}

func TestCodec_Default(t *testing.T) {
	c, err := NewCodec("")
	require.NoError(t, err)
	assert.Equal(t, "zstd", c.Name())
	assert.Equal(t, ".zst", c.Extension())
}

func TestCodec_Unknown(t *testing.T) {
	_, err := NewCodec("snappy")
	assert.Error(t, err)
}
