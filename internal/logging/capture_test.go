package logging

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponseCapture_CapturesStatusAndBody(t *testing.T) {
	rec := httptest.NewRecorder()
	cap := NewResponseCapture(rec, 1<<20)

	cap.WriteHeader(201)
	n, err := cap.Write([]byte(`{"data":"test"}`))
	require.NoError(t, err)
	assert.Equal(t, 15, n)

	assert.Equal(t, 201, cap.StatusCode())
	assert.Equal(t, `{"data":"test"}`, string(cap.CapturedBody()))
	assert.False(t, cap.Overflow())

	// Verify it also wrote through to the underlying writer
	assert.Equal(t, 201, rec.Code)
	assert.Equal(t, `{"data":"test"}`, rec.Body.String())
}

func TestResponseCapture_DefaultStatusCode(t *testing.T) {
	rec := httptest.NewRecorder()
	cap := NewResponseCapture(rec, 1<<20)

	cap.Write([]byte("body"))

	assert.Equal(t, 200, cap.StatusCode())
}

func TestResponseCapture_OverflowProtection(t *testing.T) {
	rec := httptest.NewRecorder()
	maxSize := 10
	cap := NewResponseCapture(rec, maxSize)

	// Write more than maxSize
	data := strings.Repeat("a", 50)
	n, err := cap.Write([]byte(data))
	require.NoError(t, err)
	assert.Equal(t, 50, n) // Full data written to client

	assert.True(t, cap.Overflow())
	assert.Equal(t, maxSize, len(cap.CapturedBody())) // Capture truncated
	assert.Equal(t, 50, rec.Body.Len())                // Client got everything
}

func TestResponseCapture_HeaderPassthrough(t *testing.T) {
	rec := httptest.NewRecorder()
	cap := NewResponseCapture(rec, 1<<20)

	cap.Header().Set("X-Custom", "value")
	cap.WriteHeader(200)

	assert.Equal(t, "value", rec.Header().Get("X-Custom"))
}

func TestResponseCapture_MultipleWrites(t *testing.T) {
	rec := httptest.NewRecorder()
	cap := NewResponseCapture(rec, 1<<20)

	cap.Write([]byte("hello "))
	cap.Write([]byte("world"))

	assert.Equal(t, "hello world", string(cap.CapturedBody()))
	assert.Equal(t, "hello world", rec.Body.String())
}
