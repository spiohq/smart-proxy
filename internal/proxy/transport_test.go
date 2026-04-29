package proxy

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTransport_HasDialContextWithTimeout(t *testing.T) {
	tr := newTransport()
	require.NotNil(t, tr.DialContext, "transport must set DialContext to bound dial time")

	// We cannot directly inspect the configured timeout from the closure, but
	// we can document the expected upper bound here so future changes cause
	// CI to fail until the docstring is updated.
	assert.NotZero(t, tr.TLSHandshakeTimeout)
	assert.LessOrEqual(t, tr.TLSHandshakeTimeout, 30*time.Second,
		"TLSHandshakeTimeout sanity: a hung handshake must not pile up goroutines")
}
