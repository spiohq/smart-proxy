package logging

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/spiohq/smart-proxy/internal/bodies"
	"github.com/spiohq/smart-proxy/internal/cache"
	"github.com/spiohq/smart-proxy/internal/merchant"
	"github.com/spiohq/smart-proxy/internal/pii"
	"github.com/spiohq/smart-proxy/internal/storage"
)

// DefaultMaxCaptureSize is the per-message byte cap used when LoggingMiddleware
// is constructed without an explicit size (e.g. tests). Matches the config
// default of 256 KiB.
const DefaultMaxCaptureSize = 256 * 1024

// LoggingMiddleware returns a middleware that captures request/response data
// and sends it to the AsyncLogger for non-blocking storage.
// region is the SP-API region this handler serves (e.g., "eu", "na", "fe").
// maxCaptureSize caps the per-message body bytes retained for logging; values
// <= 0 fall back to DefaultMaxCaptureSize.
func LoggingMiddleware(logger *AsyncLogger, piiRegistry *pii.Registry, region string, maxCaptureSize int64) func(http.Handler) http.Handler {
	if maxCaptureSize <= 0 {
		maxCaptureSize = DefaultMaxCaptureSize
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startTime := time.Now().UTC()

			// Generate request ID and store in context for cache middleware
			requestID := GenerateRequestID()
			ctx := cache.ContextWithRequestID(r.Context(), requestID)
			r = r.WithContext(ctx)

			// Wrap response writer to capture status + body
			capture := NewResponseCapture(w, int(maxCaptureSize))

			// Capture request body for mutations (POST/PUT/PATCH)
			var requestBody []byte
			if r.Body != nil && r.ContentLength > 0 && r.ContentLength <= maxCaptureSize {
				switch r.Method {
				case http.MethodPost, http.MethodPut, http.MethodPatch:
					requestBody, _ = io.ReadAll(io.LimitReader(r.Body, maxCaptureSize))
					r.Body.Close()
					r.Body = io.NopCloser(bytes.NewReader(requestBody))
				}
			}

			// Serve the request through the rest of the chain
			next.ServeHTTP(capture, r)

			// Skip logging if the client disconnected  -  the request never
			// reached upstream and the log entry would just be noise.
			if capture.Header().Get("X-SP-Proxy-Error-Reason") == "client_disconnected" {
				return
			}

			// Build metadata
			totalLatency := time.Since(startTime).Milliseconds()
			m := merchant.MerchantFromContext(r.Context())
			cacheStatus := capture.Header().Get("X-SP-Proxy-Cache")

			meta := &storage.RequestLog{
				ID:                    requestID,
				Timestamp:             startTime,
				MerchantKey:           m.Key,
				Region:                region,
				Method:                r.Method,
				Path:                  r.URL.Path,
				QueryParams:           r.URL.RawQuery,
				RequestHeaders:        headerToMap(pii.RedactHeaders(r.Header)),
				StatusCode:            capture.StatusCode(),
				ResponseHeaders:       headerToMap(capture.Header()),
				CacheStatus:           cacheStatus,
				TotalLatencyMs:        totalLatency,
				RequestContentLength:  r.ContentLength,
				ResponseContentLength: int64(capture.BytesWritten()),
			}

			// Amazon request ID (different header names used by different APIs)
			if rid := capture.Header().Get("x-amz-request-id"); rid != "" {
				meta.AmazonRequestID = rid
			} else if rid := capture.Header().Get("x-amzn-RequestId"); rid != "" {
				meta.AmazonRequestID = rid
			}

			// Queue info from rate limiter
			if capture.Header().Get("X-SP-Proxy-Queued") == "true" {
				meta.Queued = true
				if ms, err := strconv.ParseInt(capture.Header().Get("X-SP-Proxy-Queue-Wait-Ms"), 10, 64); err == nil {
					meta.QueueWaitMs = ms
				}
			}

			// Error reason (set by proxy ErrorHandler for 502s)
			if reason := capture.Header().Get("X-SP-Proxy-Error-Reason"); reason != "" {
				meta.ErrorReason = reason
			}

			// Upstream latency approximation
			meta.UpstreamLatencyMs = totalLatency - meta.QueueWaitMs

			// Build log entry
			entry := &LogEntry{Meta: meta}

			if cacheStatus == "HIT" {
				// Cache hit  -  no body, just reference to original
				meta.CachedFromID = capture.Header().Get("X-SP-Proxy-Cache-Source-ID")
			} else {
				// Cache miss/bypass/pii_excluded  -  capture body
				var bodyEntry *bodies.BodyEntry
				responseBody := decompressIfGzip(capture.CapturedBody(), capture.Header())

				if len(responseBody) > 0 || len(requestBody) > 0 {
					bodyEntry = &bodies.BodyEntry{ID: requestID}
					if len(responseBody) > 0 {
						bodyEntry.ResponseBody = json.RawMessage(responseBody)
					}
					if len(requestBody) > 0 {
						bodyEntry.RequestBody = json.RawMessage(requestBody)
					}
				}

				// Check PII
				if piiRegistry.ContainsPII(r) {
					meta.PIIRedacted = true
				}

				entry.Body = bodyEntry
			}

			logger.Log(entry)
		})
	}
}

// decompressIfGzip decompresses gzip-encoded body bytes for logging.
// When the client sends Accept-Encoding: gzip, Go's ReverseProxy passes
// compressed bytes through without decoding. We need the raw JSON for storage.
func decompressIfGzip(data []byte, header http.Header) []byte {
	if len(data) == 0 {
		return data
	}
	if !strings.EqualFold(header.Get("Content-Encoding"), "gzip") {
		return data
	}
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return data
	}
	defer gr.Close()
	decoded, err := io.ReadAll(gr)
	if err != nil {
		return data
	}
	return decoded
}

// headerToMap converts http.Header to map[string]string (first value only).
func headerToMap(h http.Header) map[string]string {
	m := make(map[string]string, len(h))
	for k, vals := range h {
		if len(vals) > 0 {
			m[k] = vals[0]
		}
	}
	return m
}
