package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"

	"github.com/spiohq/smart-proxy/internal/merchant"
	"github.com/spiohq/smart-proxy/internal/ratelimit"
)

func newResponseModifier() func(*http.Response) error {
	return newResponseModifierWithLimiter(nil)
}

func newResponseModifierWithLimiter(limiter *ratelimit.Limiter) func(*http.Response) error {
	return func(resp *http.Response) error {
		if m := merchant.MerchantFromContext(resp.Request.Context()); m.Key != "" {
			resp.Header.Set("X-SP-Proxy-Merchant-Key", m.Key)

			if limiter != nil {
				endpoint := ratelimit.ClassifyEndpoint(resp.Request.URL.Path)
				limiter.UpdateFromResponse(m.Key, resp.Request.Method, endpoint, resp)
			}
		}
		return nil
	}
}

// internalErrorReasonKey is a context key carrying the rich internal
// upstream-error classification from newErrorHandler to the logging
// middleware (which stores it in meta.ErrorReason). The public response
// header X-SP-Proxy-Error-Reason is intentionally coarse (F-16); using
// a context value keeps the rich taxonomy in operator logs without
// leaking it to callers.
type internalErrorReasonKey struct{}

// SetInternalErrorReason stashes the rich classification on a request
// context. Used by the logging middleware to read what the proxy's
// error handler actually saw.
func SetInternalErrorReason(r *http.Request, reason string) {
	if r == nil {
		return
	}
	if slot, ok := r.Context().Value(internalErrorReasonKey{}).(*string); ok {
		*slot = reason
	}
}

// PrepareInternalErrorReason returns a request whose context carries a
// writable string slot for the upstream error reason. The logging
// middleware calls this BEFORE invoking the proxy so the error handler
// (deeper in the chain) can record into the same slot. Returns the
// updated request and a getter that reads the current slot value.
func PrepareInternalErrorReason(r *http.Request) (*http.Request, func() string) {
	slot := new(string)
	ctx := context.WithValue(r.Context(), internalErrorReasonKey{}, slot)
	return r.WithContext(ctx), func() string { return *slot }
}

func newErrorHandler() func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		reason := classifyUpstreamError(err)
		external := externalReason(reason)

		slog.Error("upstream request failed",
			"method", r.Method,
			"path", r.URL.Path,
			"error", err.Error(),
			"reason", reason,
			"merchant", r.Header.Get("X-SP-Proxy-Merchant-Key"),
		)

		// Stash the rich internal reason on the request context so the
		// logging middleware (outer wrapper) can pick it up. The public
		// response header gets the coarse external value only (F-16).
		SetInternalErrorReason(r, reason)

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-SP-Proxy-Error-Reason", external)
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(fmt.Sprintf(
			`{"errors":[{"code":"PROXY_ERROR","message":"upstream unavailable","detail":"%s"}]}`,
			external,
		)))
	}
}

// externalReason maps the rich internal classification to a small whitelist
// of values safe to expose to the caller. The internal taxonomy stays in
// logs and the meta.ErrorReason DB column; the external set is
// intentionally coarse so it cannot be used for reconnaissance about the
// operator's network.
//
// Pentest finding F-16.
func externalReason(internal string) string {
	switch internal {
	case "upstream_timeout", "client_disconnected", "client_disconnected_in_queue":
		return internal
	default:
		return "upstream_error"
	}
}

// classifyUpstreamError inspects the error returned by the HTTP transport
// and returns a human-readable reason string for logging and diagnostics.
func classifyUpstreamError(err error) string {
	if err == nil {
		return "unknown"
	}

	// Context cancellation / timeout
	if errors.Is(err, context.DeadlineExceeded) {
		return "upstream_timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "client_disconnected"
	}

	// Connection refused
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "connection_refused"
	}
	// Connection reset
	if errors.Is(err, syscall.ECONNRESET) {
		return "connection_reset"
	}

	// URL parse errors
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Timeout() {
			return "upstream_timeout"
		}
		// Unwrap and continue classification
		err = urlErr.Err
	}

	// Net errors (DNS, dial, etc.)
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return "upstream_timeout"
		}
		op := netErr.Op
		if op == "dial" {
			var dnsErr *net.DNSError
			if errors.As(netErr.Err, &dnsErr) {
				return "dns_resolution_failed"
			}
			return "connection_failed"
		}
		if op == "read" {
			return "upstream_read_error"
		}
		if op == "write" {
			return "upstream_write_error"
		}
	}

	// TLS errors
	var tlsErr *tls.CertificateVerificationError
	if errors.As(err, &tlsErr) {
		return "tls_certificate_error"
	}
	var recordErr tls.RecordHeaderError
	if errors.As(err, &recordErr) {
		return "tls_handshake_error"
	}

	// EOF = connection closed unexpectedly
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return "upstream_timeout"
	}

	s := err.Error()
	if strings.Contains(s, "tls") || strings.Contains(s, "TLS") || strings.Contains(s, "certificate") {
		return "tls_error"
	}
	if strings.Contains(s, "timeout") || strings.Contains(s, "Timeout") {
		return "upstream_timeout"
	}
	if strings.Contains(s, "EOF") {
		return "upstream_closed_connection"
	}
	if strings.Contains(s, "connection reset") {
		return "connection_reset"
	}
	if strings.Contains(s, "no such host") {
		return "dns_resolution_failed"
	}

	return "upstream_error"
}
