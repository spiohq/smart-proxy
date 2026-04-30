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
	"github.com/spiohq/smart-proxy/internal/proxy"
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

			requestID := GenerateRequestID()
			r = r.WithContext(cache.ContextWithRequestID(r.Context(), requestID))

			// Prepare a context slot for the rich internal upstream-error
			// classification (F-16). The proxy's ErrorHandler writes into
			// this slot; the public X-SP-Proxy-Error-Reason header carries
			// only the coarse externally-safe value.
			r, getInternalErrorReason := proxy.PrepareInternalErrorReason(r)

			capture := NewResponseCapture(w, int(maxCaptureSize))
			requestBody := captureRequestBody(r, maxCaptureSize)

			next.ServeHTTP(capture, r)

			// Skip logging if the client disconnected - the request never
			// reached upstream and the log entry would just be noise.
			if capture.Header().Get("X-SP-Proxy-Error-Reason") == "client_disconnected" {
				return
			}

			meta := buildRequestLog(r, capture, piiRegistry, requestID, region, startTime, getInternalErrorReason)
			entry := buildLogEntry(r, capture, piiRegistry, meta, requestID, requestBody, maxCaptureSize)
			logger.Log(entry)
		})
	}
}

// captureRequestBody snapshots the request body for mutating methods so the
// log entry can include the payload. The body is restored to a fresh reader
// before downstream handlers run. Bodies above maxCaptureSize are skipped to
// avoid pulling unbounded memory into the logging path.
func captureRequestBody(r *http.Request, maxCaptureSize int64) []byte {
	if r.Body == nil || r.ContentLength <= 0 || r.ContentLength > maxCaptureSize {
		return nil
	}
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
	default:
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, maxCaptureSize))
	r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body
}

// buildRequestLog assembles the storage.RequestLog metadata from the captured
// response. getInternalErrorReason is the closure returned by
// proxy.PrepareInternalErrorReason and reads back the rich F-16 reason after
// the chain has run.
func buildRequestLog(r *http.Request, capture *ResponseCapture, piiRegistry *pii.Registry, requestID, region string, startTime time.Time, getInternalErrorReason func() string) *storage.RequestLog {
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
		QueryParams:           pii.RedactQueryString(r.URL.RawQuery, piiRegistry.QueryParamsExtra()),
		StatusCode:            capture.StatusCode(),
		CacheStatus:           cacheStatus,
		TotalLatencyMs:        totalLatency,
		RequestContentLength:  r.ContentLength,
		ResponseContentLength: int64(capture.BytesWritten()),
	}

	// Amazon request ID (different header names used by different APIs).
	if rid := capture.Header().Get("x-amz-request-id"); rid != "" {
		meta.AmazonRequestID = rid
	} else if rid := capture.Header().Get("x-amzn-RequestId"); rid != "" {
		meta.AmazonRequestID = rid
	}

	if capture.Header().Get("X-SP-Proxy-Queued") == "true" {
		meta.Queued = true
		if ms, err := strconv.ParseInt(capture.Header().Get("X-SP-Proxy-Queue-Wait-Ms"), 10, 64); err == nil {
			meta.QueueWaitMs = ms
		}
	}

	// F-16: prefer the rich internal classification (not exposed to the
	// caller); fall back to the coarse public header for paths that don't
	// go through the upstream ErrorHandler (e.g. rate-limit's
	// "client_disconnected_in_queue", set directly on the response header).
	if reason := getInternalErrorReason(); reason != "" {
		meta.ErrorReason = reason
	} else if reason := capture.Header().Get("X-SP-Proxy-Error-Reason"); reason != "" {
		meta.ErrorReason = reason
	}

	meta.UpstreamLatencyMs = totalLatency - meta.QueueWaitMs
	return meta
}

// buildLogEntry attaches the captured headers and (when the request was a
// cache miss) the response/request bodies to a LogEntry. Cache hits store
// only a back-reference to the original entry's body.
func buildLogEntry(r *http.Request, capture *ResponseCapture, piiRegistry *pii.Registry, meta *storage.RequestLog, requestID string, requestBody []byte, maxCaptureSize int64) *LogEntry {
	reqHeaders := headerToMap(pii.RedactHeaders(r.Header))
	// Response headers go through the same SensitiveHeaders filter as
	// request headers (F-12). Amazon does not normally emit session-bearing
	// values, but the symmetry is defensive.
	respHeaders := headerToMap(pii.RedactHeaders(capture.Header()))

	entry := &LogEntry{Meta: meta}

	if meta.CacheStatus == "HIT" {
		// Cache hit: no body, just reference to original. Headers still go
		// into a BodyEntry so they can be retrieved from JSONL alongside
		// the original response's payload.
		meta.CachedFromID = capture.Header().Get("X-SP-Proxy-Cache-Source-ID")
		entry.Body = &bodies.BodyEntry{
			ID:              requestID,
			RequestHeaders:  reqHeaders,
			ResponseHeaders: respHeaders,
		}
		return entry
	}

	responseBody := decompressIfGzipBounded(capture.CapturedBody(), capture.Header(), 4*maxCaptureSize)
	bodyEntry := &bodies.BodyEntry{
		ID:              requestID,
		RequestHeaders:  reqHeaders,
		ResponseHeaders: respHeaders,
	}
	if len(responseBody) > 0 {
		bodyEntry.ResponseBody = json.RawMessage(responseBody)
	}
	if len(requestBody) > 0 {
		bodyEntry.RequestBody = json.RawMessage(requestBody)
	}

	if piiRegistry.ContainsPII(r) {
		meta.PIIRedactedResponse = true
	}
	if piiRegistry.RequestBodyContainsPII(r) {
		meta.PIIRedactedRequest = true
		entry.RequestBodyEndpoint = piiRegistry.RequestBodyPattern(r)
	}
	entry.Body = bodyEntry
	return entry
}

// decompressIfGzip decompresses gzip-encoded body bytes for logging using
// the default decompression cap of 4 * DefaultMaxCaptureSize. When the client
// sends Accept-Encoding: gzip, Go's ReverseProxy passes compressed bytes
// through without decoding -- we need the raw JSON for storage.
func decompressIfGzip(data []byte, header http.Header) []byte {
	return decompressIfGzipBounded(data, header, 4*int64(DefaultMaxCaptureSize))
}

// decompressIfGzipBounded is like decompressIfGzip but caps the decompressed
// output at maxOut bytes. This defends against gzip-bomb amplification: a
// 256 KiB highly-compressed payload can otherwise expand into hundreds of MB
// in process memory. On overflow the function returns the original
// (compressed) bytes -- the redaction engine will detect non-JSON content
// downstream and skip its rule walk, which is the correct fail-soft choice.
//
// Pentest finding F-09.
func decompressIfGzipBounded(data []byte, header http.Header, maxOut int64) []byte {
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
	// io.LimitReader caps the decompressed read at maxOut+1 to detect
	// overflow: if we read maxOut+1 bytes, the input would have produced
	// more, so we treat that as a bomb and fall back to the compressed
	// bytes rather than partially decoded data.
	limited := io.LimitReader(gr, maxOut+1)
	decoded, err := io.ReadAll(limited)
	if err != nil {
		return data
	}
	if int64(len(decoded)) > maxOut {
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
