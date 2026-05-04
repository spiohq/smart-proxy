package logging

import (
	"bytes"
	"net/http"
)

// ResponseCapture wraps an http.ResponseWriter to capture the response
// while writing through to the client. The client always receives the
// full response  -  maxSize only limits how much body we capture for logging.
type ResponseCapture struct {
	http.ResponseWriter
	body         *bytes.Buffer
	statusCode   int
	maxSize      int64
	overflow     bool
	wroteHead    bool
	bytesWritten int
}

// NewResponseCapture creates a ResponseCapture with the given max capture size.
func NewResponseCapture(w http.ResponseWriter, maxSize int64) *ResponseCapture {
	return &ResponseCapture{
		ResponseWriter: w,
		body:           &bytes.Buffer{},
		statusCode:     200,
		maxSize:        maxSize,
	}
}

// WriteHeader captures the status code and delegates to the underlying writer.
func (c *ResponseCapture) WriteHeader(code int) {
	if !c.wroteHead {
		c.statusCode = code
		c.wroteHead = true
	}
	c.ResponseWriter.WriteHeader(code)
}

// Write writes to the client AND captures up to maxSize bytes for logging.
func (c *ResponseCapture) Write(b []byte) (int, error) {
	if !c.overflow {
		remaining := c.maxSize - int64(c.body.Len())
		if remaining <= 0 {
			c.overflow = true
		} else if int64(len(b)) <= remaining {
			c.body.Write(b)
		} else {
			c.body.Write(b[:remaining])
			c.overflow = true
		}
	}
	n, err := c.ResponseWriter.Write(b)
	c.bytesWritten += n
	return n, err
}

// StatusCode returns the captured HTTP status code.
func (c *ResponseCapture) StatusCode() int {
	return c.statusCode
}

// CapturedBody returns the captured response body (up to maxSize).
func (c *ResponseCapture) CapturedBody() []byte {
	return c.body.Bytes()
}

// Overflow returns true if the response body exceeded maxSize.
func (c *ResponseCapture) Overflow() bool {
	return c.overflow
}

// BytesWritten returns the total bytes written to the client.
func (c *ResponseCapture) BytesWritten() int {
	return c.bytesWritten
}
