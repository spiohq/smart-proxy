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

func newErrorHandler() func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		reason := classifyUpstreamError(err)

		slog.Error("upstream request failed",
			"method", r.Method,
			"path", r.URL.Path,
			"error", err.Error(),
			"reason", reason,
			"merchant", r.Header.Get("X-SP-Proxy-Merchant-Key"),
		)

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-SP-Proxy-Error-Reason", reason)
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(fmt.Sprintf(
			`{"errors":[{"code":"PROXY_ERROR","message":"upstream unavailable","detail":"%s"}]}`,
			reason,
		)))
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
