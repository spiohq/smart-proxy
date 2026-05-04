package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSecurityHeadersMiddleware_AddsAllHeaders(t *testing.T) {
	h := SecurityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("GET", "/api/v1/logs", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	hdr := rec.Header()
	assert.Equal(t, "no-store, no-cache, must-revalidate", hdr.Get("Cache-Control"))
	assert.Equal(t, "DENY", hdr.Get("X-Frame-Options"))
	assert.Equal(t, "nosniff", hdr.Get("X-Content-Type-Options"))
	assert.Equal(t, "no-referrer", hdr.Get("Referrer-Policy"))

	csp := hdr.Get("Content-Security-Policy")
	for _, expected := range []string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-inline'",
		"style-src 'self' 'unsafe-inline'",
		"font-src 'self' data:",
		"frame-ancestors 'none'",
		"base-uri 'self'",
		"form-action 'self'",
	} {
		assert.True(t, strings.Contains(csp, expected),
			"CSP must contain %q -- got %q", expected, csp)
	}
}

func TestSecurityHeadersMiddleware_PassesThroughBody(t *testing.T) {
	// Sanity: the middleware does not eat the body.
	h := SecurityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"hello":"world"}`))
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, `{"hello":"world"}`, rec.Body.String())
}

func TestSecurityHeadersMiddleware_PreservesHandlerHeaders(t *testing.T) {
	// Sanity: existing headers set by the inner handler remain.
	h := SecurityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Custom", "from-handler")
		w.WriteHeader(http.StatusAccepted)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Equal(t, "from-handler", rec.Header().Get("X-Custom"))
	// And the security headers are still there.
	assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
}
