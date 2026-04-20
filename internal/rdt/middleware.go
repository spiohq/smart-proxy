package rdt

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/spiohq/smart-proxy/internal/merchant"
	"golang.org/x/sync/singleflight"
)

const rdtTokenPrefix = "Atz.sprdt|"

// forceRDTHeader allows clients to override automatic RDT behavior per request.
// "true" = force mint an RDT (useful for report documents from notifications).
// "false" = skip all RDT logic, pass through the original token.
// Absent = normal auto-detection flow.
// The header is always stripped before forwarding to upstream.
const forceRDTHeader = "X-SP-Proxy-Force-RDT"

// Report-related path prefixes for sniffing.
const (
	reportsBasePath   = "/reports/2021-06-30/reports"
	documentsBasePath = "/reports/2021-06-30/documents/"
)

// Middleware handles automatic RDT minting and caching for PII endpoints.
// When a request targets a restricted SP-API operation and the client sends
// a regular LWA token (not an RDT), the middleware looks up a cached RDT or
// mints a new one, then swaps the token header before forwarding.
//
// For report documents, the middleware sniffs the POST /reports and
// GET /reports/{id} flows to correlate reportType with documentId, then
// mints with the concrete document path (Amazon rejects generic paths).
//
// If minting fails, the middleware fails open: the original request is
// forwarded unchanged. If the upstream returns 403 after a token swap,
// the cache entry is invalidated so the next request gets a fresh RDT.
type Middleware struct {
	cache   *Cache
	minter  *Minter
	reports *ReportTracker
	group   singleflight.Group
}

// NewMiddleware creates a new RDT middleware. If minter is nil, the middleware
// is disabled and all requests pass through unchanged (feature off).
// reports may be nil if report tracking is not needed.
func NewMiddleware(cache *Cache, minter *Minter, reports *ReportTracker) *Middleware {
	return &Middleware{
		cache:   cache,
		minter:  minter,
		reports: reports,
	}
}

// Handler returns an http.Handler middleware that wraps the given next handler.
func (m *Middleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Feature off: pass through
		if m.minter == nil {
			next.ServeHTTP(w, r)
			return
		}

		// Handle X-SP-Proxy-Force-RDT override header.
		// Always strip before forwarding to upstream.
		forceValue := r.Header.Get(forceRDTHeader)
		r.Header.Del(forceRDTHeader)

		if forceValue == "false" {
			// Explicit opt-out: skip all RDT logic
			next.ServeHTTP(w, r)
			return
		}

		if forceValue == "true" {
			// Explicit opt-in: mint with concrete path, skip matchers
			m.handleForceRDT(w, r, next)
			return
		}

		// Report sniffing: intercept POST /reports and GET /reports/{id}
		// to track reportType -> documentId correlation.
		if m.reports != nil {
			if m.handleReportSniffing(w, r, next) {
				return
			}
		}

		// Check if this is a PII endpoint (non-report)
		op, isPII := MatchPIIOperation(r.Method, r.URL.Path)
		if !isPII {
			next.ServeHTTP(w, r)
			return
		}

		// Check if client already sends an RDT
		token := r.Header.Get("x-amz-access-token")
		if strings.HasPrefix(token, rdtTokenPrefix) {
			next.ServeHTTP(w, r)
			return
		}

		// Resolve merchant for cache key
		merch := merchant.MerchantFromContext(r.Context())
		cacheKey := BuildCacheKey(merch.Key, op)

		// Try cache (only for cacheable operations)
		if op.Cacheable {
			if entry, ok := m.cache.Get(cacheKey); ok {
				r.Header.Set("x-amz-access-token", entry.Token)
				rec := &statusRecorder{ResponseWriter: w}
				next.ServeHTTP(rec, r)
				if rec.status == http.StatusForbidden || rec.status == http.StatusUnauthorized {
					m.cache.Invalidate(cacheKey)
				}
				return
			}
		}

		// Cache miss: mint a new RDT (with singleflight deduplication)
		sfKey := cacheKey.MerchantID + "|" + cacheKey.GenericPath + "|" + cacheKey.DataElements
		result, _, _ := m.group.Do(sfKey, func() (any, error) {
			// Double-check cache after acquiring singleflight slot
			if op.Cacheable {
				if entry, ok := m.cache.Get(cacheKey); ok {
					return entry, nil
				}
			}

			entry, err := m.minter.Mint(token, op.ToRestrictedResource())
			if err != nil {
				slog.Warn("rdt: mint failed, failing open", "error", err, "path", op.GenericPath, "merchant", merch.Key)
				return nil, err
			}

			if op.Cacheable {
				m.cache.Set(cacheKey, entry)
			}
			return entry, nil
		})

		if result == nil {
			// Mint failed: fail open with original token
			next.ServeHTTP(w, r)
			return
		}

		entry := result.(CacheEntry)
		r.Header.Set("x-amz-access-token", entry.Token)

		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)

		// Invalidate on 401/403 from upstream
		if rec.status == http.StatusForbidden || rec.status == http.StatusUnauthorized {
			m.cache.Invalidate(cacheKey)
		}
	})
}

// handleForceRDT handles requests with X-SP-Proxy-Force-RDT: true.
// It mints an RDT using the concrete request path, regardless of whether
// the endpoint is in the PII matcher table. If the token is already an RDT,
// it passes through without minting. On mint failure, it fails open.
func (m *Middleware) handleForceRDT(w http.ResponseWriter, r *http.Request, next http.Handler) {
	token := r.Header.Get("x-amz-access-token")
	if strings.HasPrefix(token, rdtTokenPrefix) {
		next.ServeHTTP(w, r)
		return
	}

	// Strip query string for the RDT path
	concretePath := r.URL.Path
	if idx := strings.IndexByte(concretePath, '?'); idx != -1 {
		concretePath = concretePath[:idx]
	}

	resource := RestrictedResource{
		Method: r.Method,
		Path:   concretePath,
	}

	entry, err := m.minter.Mint(token, resource)
	if err != nil {
		slog.Warn("rdt: forced mint failed, failing open", "error", err, "path", concretePath)
		next.ServeHTTP(w, r)
		return
	}

	r.Header.Set("x-amz-access-token", entry.Token)
	next.ServeHTTP(w, r)
}

// handleReportSniffing handles the 3-step report flow. Returns true if the
// request was handled (caller should return), false if it should continue
// to the normal PII matcher flow.
func (m *Middleware) handleReportSniffing(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
	path := r.URL.Path

	// Step 1: POST /reports -> sniff reportType from request body
	if r.Method == "POST" && path == reportsBasePath {
		m.sniffCreateReport(w, r, next)
		return true
	}

	// Step 2: GET /reports/{reportId} -> sniff documentId from response
	if r.Method == "GET" && strings.HasPrefix(path, reportsBasePath+"/") && !strings.HasPrefix(path, documentsBasePath) {
		m.sniffGetReport(w, r, next)
		return true
	}

	// Step 3: GET /documents/{docId} -> mint if restricted
	if r.Method == "GET" && strings.HasPrefix(path, documentsBasePath) {
		return m.handleDocumentDownload(w, r, next)
	}

	return false
}

// sniffCreateReport intercepts POST /reports to extract reportType from the
// request body and reportId from the response body.
func (m *Middleware) sniffCreateReport(w http.ResponseWriter, r *http.Request, next http.Handler) {
	// Read and restore request body to extract reportType
	var reportType string
	if r.Body != nil {
		bodyBytes, err := io.ReadAll(r.Body)
		r.Body.Close()
		if err == nil {
			var reqBody struct {
				ReportType string `json:"reportType"`
			}
			json.Unmarshal(bodyBytes, &reqBody)
			reportType = reqBody.ReportType
		}
		// Restore body for downstream
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	// Capture response to extract reportId
	rec := &bodyRecorder{ResponseWriter: w}
	next.ServeHTTP(rec, r)

	if reportType != "" && rec.statusCode() < 400 {
		var respBody struct {
			ReportID string `json:"reportId"`
		}
		if json.Unmarshal(rec.body.Bytes(), &respBody) == nil && respBody.ReportID != "" {
			m.reports.TrackReportCreation(respBody.ReportID, reportType)
		}
	}
}

// sniffGetReport intercepts GET /reports/{reportId} to extract
// reportDocumentId from the response and link it to the reportType.
func (m *Middleware) sniffGetReport(w http.ResponseWriter, r *http.Request, next http.Handler) {
	// Extract reportId from path: /reports/2021-06-30/reports/{reportId}
	reportID := strings.TrimPrefix(r.URL.Path, reportsBasePath+"/")
	if idx := strings.IndexByte(reportID, '/'); idx != -1 {
		reportID = reportID[:idx]
	}

	// Capture response to extract reportDocumentId
	rec := &bodyRecorder{ResponseWriter: w}
	next.ServeHTTP(rec, r)

	if rec.statusCode() < 400 {
		var respBody struct {
			ReportDocumentID string `json:"reportDocumentId"`
		}
		if json.Unmarshal(rec.body.Bytes(), &respBody) == nil && respBody.ReportDocumentID != "" {
			m.reports.TrackReportDocument(reportID, respBody.ReportDocumentID)
		}
	}
}

// handleDocumentDownload handles GET /documents/{docId}. If the document
// belongs to a restricted report type, it mints an RDT with the concrete
// path and swaps the token. Returns true if handled.
func (m *Middleware) handleDocumentDownload(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
	// Extract documentId
	docID := strings.TrimPrefix(r.URL.Path, documentsBasePath)
	if docID == "" {
		next.ServeHTTP(w, r)
		return true
	}

	// Check if client already sends an RDT
	token := r.Header.Get("x-amz-access-token")
	if strings.HasPrefix(token, rdtTokenPrefix) {
		next.ServeHTTP(w, r)
		return true
	}

	// Look up report type
	reportType, known := m.reports.LookupDocumentReportType(docID)
	if !known || !IsRestrictedReportType(reportType) {
		// Unknown doc or non-restricted type: pass through
		next.ServeHTTP(w, r)
		return true
	}

	// Restricted report: mint RDT with concrete path
	concretePath := documentsBasePath + docID
	resource := RestrictedResource{
		Method: "GET",
		Path:   concretePath,
	}

	entry, err := m.minter.Mint(token, resource)
	if err != nil {
		slog.Warn("rdt: report mint failed, failing open", "error", err, "documentId", docID)
		next.ServeHTTP(w, r)
		return true
	}

	r.Header.Set("x-amz-access-token", entry.Token)
	next.ServeHTTP(w, r)
	return true
}

// statusRecorder wraps http.ResponseWriter to capture the status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// bodyRecorder captures the response body while still writing it to the
// underlying ResponseWriter. Used for sniffing report API responses.
type bodyRecorder struct {
	http.ResponseWriter
	body   bytes.Buffer
	status int
}

func (r *bodyRecorder) statusCode() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}

func (r *bodyRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *bodyRecorder) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}
