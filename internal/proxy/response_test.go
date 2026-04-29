package proxy

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Pentest finding F-16: the public X-SP-Proxy-Error-Reason header is
// coarsened to a small whitelist so callers cannot fingerprint the
// operator's network. The rich classification stays in logs and on
// the request context for the logging middleware to persist.

func TestExternalReason_WhitelistedValuesPassThrough(t *testing.T) {
	cases := map[string]string{
		"upstream_timeout":             "upstream_timeout",
		"client_disconnected":          "client_disconnected",
		"client_disconnected_in_queue": "client_disconnected_in_queue",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			assert.Equal(t, want, externalReason(in))
		})
	}
}

func TestExternalReason_NonWhitelistedCollapsedToUpstreamError(t *testing.T) {
	cases := []string{
		"tls_certificate_error",
		"dns_resolution_failed",
		"connection_refused",
		"connection_reset",
		"upstream_read_error",
		"upstream_write_error",
		"upstream_closed_connection",
		"tls_handshake_error",
		"unknown",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			assert.Equal(t, "upstream_error", externalReason(in),
				"reconnaissance-grade reason %q must coarsen to upstream_error", in)
		})
	}
}

func TestPrepareInternalErrorReason_RoundTrip(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r2, getter := PrepareInternalErrorReason(r)

	// Empty before anyone writes.
	assert.Empty(t, getter())

	SetInternalErrorReason(r2, "tls_certificate_error")
	assert.Equal(t, "tls_certificate_error", getter())
}

func TestSetInternalErrorReason_NoSlot_NoOp(t *testing.T) {
	// If the request never went through PrepareInternalErrorReason, the
	// setter is a silent no-op. Nothing crashes.
	r := httptest.NewRequest("GET", "/", nil)
	SetInternalErrorReason(r, "anything")
}

func TestSetInternalErrorReason_NilRequest_NoOp(t *testing.T) {
	// Defensive: nil request must not panic.
	SetInternalErrorReason(nil, "anything")
}

func TestPrepareInternalErrorReason_PreservesExistingContext(t *testing.T) {
	// Make sure stashing the slot does not overwrite or break other
	// values already on the context (e.g. cache request ID).
	type otherKey struct{}
	r := httptest.NewRequest("GET", "/", nil)
	r = r.WithContext(context.WithValue(r.Context(), otherKey{}, "preserved"))

	r2, _ := PrepareInternalErrorReason(r)
	assert.Equal(t, "preserved", r2.Context().Value(otherKey{}))
}
