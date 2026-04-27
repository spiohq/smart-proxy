package blob

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestS3Integration_NewS3PropagatesSSEHeader verifies that NewS3 (the real
// constructor) produces a backend whose PutObject calls hit the wire with the
// configured x-amz-server-side-encryption header. This goes through the full
// AWS SDK signing pipeline, unlike the unit tests which use the fakeS3
// in-process mock.
//
// We point the SDK at an httptest.Server and capture the request headers
// directly, then assert the header survives serialization.
func TestS3Integration_NewS3PropagatesSSEHeader(t *testing.T) {
	tests := []struct {
		name        string
		sse         string
		kmsKeyID    string
		wantSSE     string
		wantKMSKey  string
	}{
		{name: "no SSE configured -> no header", sse: "", wantSSE: ""},
		{name: "AES256", sse: "AES256", wantSSE: "AES256"},
		{
			name:       "aws:kms with key",
			sse:        "aws:kms",
			kmsKeyID:   "alias/proxy-bodies",
			wantSSE:    "aws:kms",
			wantKMSKey: "alias/proxy-bodies",
		},
		{name: "aws:kms:dsse no key", sse: "aws:kms:dsse", wantSSE: "aws:kms:dsse"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured http.Header
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPut {
					captured = r.Header.Clone()
				}
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(ts.Close)

			b, err := NewS3(context.Background(), S3Options{
				Bucket:      "bucket",
				Region:      "us-east-1",
				Endpoint:    ts.URL,
				AccessKey:   "AKIAIOSFODNN7EXAMPLE",
				SecretKey:   "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				PathStyle:   true,
				SSE:         tt.sse,
				SSEKMSKeyID: tt.kmsKeyID,
			})
			require.NoError(t, err)

			payload := []byte("hello")
			err = b.Put(context.Background(), "k", bytes.NewReader(payload), int64(len(payload)))
			require.NoError(t, err)
			require.NotNil(t, captured, "httptest server did not receive a PUT")

			// AWS canonicalizes header names to lowercase on the wire but the
			// stdlib http.Header preserves them via CanonicalMIMEHeaderKey, so
			// look up via Get() which is case-insensitive.
			assert.Equal(t, tt.wantSSE, captured.Get("X-Amz-Server-Side-Encryption"))
			if tt.wantKMSKey != "" {
				assert.Equal(t, tt.wantKMSKey, captured.Get("X-Amz-Server-Side-Encryption-Aws-Kms-Key-Id"))
			} else {
				assert.Empty(t, captured.Get("X-Amz-Server-Side-Encryption-Aws-Kms-Key-Id"))
			}
		})
	}
}

// TestS3Integration_NewS3RejectsInvalidSSEUpFront ensures NewS3 itself fails
// fast on a bogus SSE value rather than deferring to the first PutObject.
func TestS3Integration_NewS3RejectsInvalidSSEUpFront(t *testing.T) {
	_, err := NewS3(context.Background(), S3Options{
		Bucket: "bucket",
		Region: "us-east-1",
		SSE:    "rot13",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported SSE")
}
