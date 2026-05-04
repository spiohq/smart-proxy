package dashboard

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/spiohq/smart-proxy/internal/bodies"
	"github.com/spiohq/smart-proxy/internal/endpoint"
	"github.com/spiohq/smart-proxy/internal/storage"
)

// statusFilterRe constrains the ?status= query parameter on /api/v1/logs.
// Three-digit codes (200, 404) or Nxx buckets (4xx, 5xx) only.
//
// Pentest finding F-18.
var statusFilterRe = regexp.MustCompile(`^([1-5][0-9]{2}|[1-5]xx)$`)

func (h *Handler) handleLogs(w http.ResponseWriter, r *http.Request) {
	from, to, err := parseTimeRange(r, 1*time.Hour)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid time range: "+err.Error())
		return
	}

	limit, err := parseInt(r, "limit", 50)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid limit: "+err.Error())
		return
	}
	offset, err := parseInt(r, "offset", 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid offset: "+err.Error())
		return
	}

	var minLatency, maxLatency int64
	if v, err := parseInt(r, "minLatency", 0); err != nil {
		writeError(w, http.StatusBadRequest, "invalid minLatency: "+err.Error())
		return
	} else {
		minLatency = int64(v)
	}
	if v, err := parseInt(r, "maxLatency", 0); err != nil {
		writeError(w, http.StatusBadRequest, "invalid maxLatency: "+err.Error())
		return
	} else {
		maxLatency = int64(v)
	}

	// Validate status filter (F-18). SQLite silently coerces non-numeric
	// strings to 0 against an INTEGER column, so "?status=garbage"
	// quietly returns rows where status_code=0 (i.e. nothing). Reject
	// anything that's not a three-digit code or an Nxx bucket so the
	// behavior is unambiguous to operators.
	statusParam := r.URL.Query().Get("status")
	if statusParam != "" && !statusFilterRe.MatchString(statusParam) {
		writeError(w, http.StatusBadRequest, "invalid status filter (want 200, 4xx, etc.)")
		return
	}

	filter := storage.LogFilter{
		From:        from,
		To:          to,
		Merchant:    r.URL.Query().Get("merchant"),
		Region:      r.URL.Query().Get("region"),
		Endpoint:    r.URL.Query().Get("endpoint"),
		Status:      statusParam,
		CacheStatus: r.URL.Query().Get("cacheStatus"),
		Method:      r.URL.Query().Get("method"),
		Queued:      r.URL.Query().Get("queued"),
		MinLatency:  minLatency,
		MaxLatency:  maxLatency,
		Limit:       limit,
		Offset:      offset,
	}

	rows, total, err := h.logStore.QueryLogs(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	entries := make([]logListEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, logListEntry{
			ID:                    row.ID,
			Timestamp:             row.Timestamp,
			MerchantKey:           row.MerchantKey,
			Region:                row.Region,
			Method:                row.Method,
			Path:                  row.Path,
			StatusCode:            row.StatusCode,
			CacheStatus:           row.CacheStatus,
			TotalLatencyMs:        row.TotalLatencyMs,
			UpstreamLatencyMs:     row.UpstreamLatencyMs,
			RequestContentLength:  row.RequestContentLength,
			ResponseContentLength: row.ResponseContentLength,
			PIIRedactedRequest:    row.PIIRedactedRequest,
			PIIRedactedResponse:   row.PIIRedactedResponse,
			PIIRedacted:           row.PIIRedactedRequest || row.PIIRedactedResponse,
			AmazonRequestID:       row.AmazonRequestID,
			ErrorReason:           row.ErrorReason,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"rows": entries, "total": total})
}

// logListEntry is a DTO for the log list endpoint, exposing only the fields
// needed by the frontend LogEntry interface with proper camelCase JSON tags.
type logListEntry struct {
	ID                    string    `json:"id"`
	Timestamp             time.Time `json:"timestamp"`
	MerchantKey           string    `json:"merchantKey"`
	Region                string    `json:"region"`
	Method                string    `json:"method"`
	Path                  string    `json:"path"`
	StatusCode            int       `json:"statusCode"`
	CacheStatus           string    `json:"cacheStatus"`
	TotalLatencyMs        int64     `json:"totalLatencyMs"`
	UpstreamLatencyMs     int64     `json:"upstreamLatencyMs"`
	RequestContentLength  int64     `json:"requestContentLength"`
	ResponseContentLength int64     `json:"responseContentLength"`
	PIIRedactedRequest    bool      `json:"piiRedactedRequest"`
	PIIRedactedResponse   bool      `json:"piiRedactedResponse"`
	PIIRedacted           bool      `json:"piiRedacted"` // legacy OR shim -- keep for one release
	AmazonRequestID       string    `json:"amazonRequestId,omitempty"`
	ErrorReason           string    `json:"errorReason,omitempty"`
}

type logDetailResponse struct {
	ID                      string            `json:"id"`
	Timestamp               time.Time         `json:"timestamp"`
	MerchantKey             string            `json:"merchantKey"`
	Region                  string            `json:"region"`
	Method                  string            `json:"method"`
	Path                    string            `json:"path"`
	QueryParams             string            `json:"queryParams,omitempty"`
	RequestHeaders          map[string]string `json:"requestHeaders,omitempty"`
	StatusCode              int               `json:"statusCode"`
	ResponseHeaders         map[string]string `json:"responseHeaders,omitempty"`
	CacheStatus             string            `json:"cacheStatus"`
	CachedFromID            string            `json:"cachedFromId,omitempty"`
	CachedFromTimestamp     *time.Time        `json:"cachedFromTimestamp,omitempty"`
	CachedFromStatus        int               `json:"cachedFromStatus,omitempty"`
	Queued                  bool              `json:"queued"`
	QueueWaitMs             int64             `json:"queueWaitMs"`
	UpstreamLatencyMs       int64             `json:"upstreamLatencyMs"`
	TotalLatencyMs          int64             `json:"totalLatencyMs"`
	RequestContentLength    int64             `json:"requestContentLength"`
	ResponseContentLength   int64             `json:"responseContentLength"`
	PIIRedactedRequest      bool              `json:"piiRedactedRequest"`
	PIIRedactedResponse     bool              `json:"piiRedactedResponse"`
	PIIRedacted             bool              `json:"piiRedacted"` // legacy OR shim -- keep for one release
	AmazonRequestID         string            `json:"amazonRequestId,omitempty"`
	ErrorReason             string            `json:"errorReason,omitempty"`
	HasBody                 bool              `json:"hasBody"`
	ReplayAvailable         bool              `json:"replayAvailable"`
	ReplayUnavailableReason string            `json:"replayUnavailableReason,omitempty"`
}

func (h *Handler) handleLogByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	entry, err := h.logStore.QueryByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if entry == nil {
		writeError(w, http.StatusNotFound, "log entry not found")
		return
	}

	hasBody := entry.BodyFile != ""

	// Headers live in the JSONL body entry alongside the payload. Fetch
	// them if we have a reference; if the body has been evicted the row
	// will not have one (rotator nullifies on delete).
	var reqHeaders, respHeaders map[string]string
	if hasBody {
		if body, berr := h.bodyStore.Read(r.Context(), entry.BodyFile, entry.BodyOffset, entry.BodyLength); berr == nil {
			reqHeaders = body.RequestHeaders
			respHeaders = body.ResponseHeaders
		}
	}

	resp := logDetailResponse{
		ID:                    entry.ID,
		Timestamp:             entry.Timestamp,
		MerchantKey:           entry.MerchantKey,
		Region:                entry.Region,
		Method:                entry.Method,
		Path:                  entry.Path,
		QueryParams:           entry.QueryParams,
		RequestHeaders:        reqHeaders,
		StatusCode:            entry.StatusCode,
		ResponseHeaders:       respHeaders,
		CacheStatus:           entry.CacheStatus,
		CachedFromID:          entry.CachedFromID,
		Queued:                entry.Queued,
		QueueWaitMs:           entry.QueueWaitMs,
		UpstreamLatencyMs:     entry.UpstreamLatencyMs,
		TotalLatencyMs:        entry.TotalLatencyMs,
		RequestContentLength:  entry.RequestContentLength,
		ResponseContentLength: entry.ResponseContentLength,
		PIIRedactedRequest:    entry.PIIRedactedRequest,
		PIIRedactedResponse:   entry.PIIRedactedResponse,
		PIIRedacted:           entry.PIIRedactedRequest || entry.PIIRedactedResponse,
		AmazonRequestID:       entry.AmazonRequestID,
		ErrorReason:           entry.ErrorReason,
		HasBody:               hasBody,
	}

	// For cache HITs, look up the original request to provide context and body access
	if entry.CachedFromID != "" && !hasBody {
		if original, err := h.logStore.QueryByID(r.Context(), entry.CachedFromID); err == nil && original != nil {
			resp.CachedFromTimestamp = &original.Timestamp
			resp.CachedFromStatus = original.StatusCode
			if original.BodyFile != "" {
				resp.HasBody = true
				if body, berr := h.bodyStore.Read(r.Context(), original.BodyFile, original.BodyOffset, original.BodyLength); berr == nil {
					resp.RequestHeaders = body.RequestHeaders
					resp.ResponseHeaders = body.ResponseHeaders
				}
			}
		}
	}

	if entry.PIIRedactedRequest {
		resp.ReplayAvailable = false
		resp.ReplayUnavailableReason = "Replay unavailable: the request body contained PII and was redacted before storage."
	} else if h.tokenStore != nil {
		if _, ok := h.tokenStore.Get(entry.MerchantKey); ok {
			resp.ReplayAvailable = true
		} else {
			resp.ReplayAvailable = false
			resp.ReplayUnavailableReason = h.tokenStore.UnavailabilityReason(entry.MerchantKey)
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleLogBody(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	entry, err := h.logStore.QueryByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if entry == nil {
		writeError(w, http.StatusNotFound, "log entry not found")
		return
	}

	// For cache HITs, follow the reference to the original request's body
	if entry.BodyFile == "" && entry.CachedFromID != "" {
		original, err := h.logStore.QueryByID(r.Context(), entry.CachedFromID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		if original != nil && original.BodyFile != "" {
			entry = original
		}
	}

	if entry.BodyFile == "" {
		writeError(w, http.StatusNotFound, "no body stored for this request")
		return
	}

	body, err := h.bodyStore.Read(r.Context(), entry.BodyFile, entry.BodyOffset, entry.BodyLength)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read body")
		return
	}

	// Read-side redaction filter (F-15). Legacy entries written before F-02
	// closed the write-side leak may contain unredacted PII; re-run the
	// engine on both directions so the API never returns stored PII even
	// when the metadata flag is misleading. After F-02 this is mostly
	// belt-and-suspenders against future write-side bugs.
	if h.piiEngine != nil {
		h.redactStoredBody(body, entry)
	}

	resp := map[string]json.RawMessage{
		"requestBody":  body.RequestBody,
		"responseBody": body.ResponseBody,
	}
	writeJSON(w, http.StatusOK, resp)
}

// redactStoredBody re-runs the PII engine on both directions of a stored
// request/response pair before returning it to the dashboard caller. See the
// call site for the F-02 / F-15 motivation.
func (h *Handler) redactStoredBody(body *bodies.BodyEntry, entry *storage.RequestLog) {
	classified, known := endpoint.ClassifyKnown(entry.Path)
	reg := h.piiEngine.Registry()

	// Response side: same logic the async logger uses.
	if body.ResponseBody != nil {
		if reg.IsFullBodyPII(classified, known) {
			body.ResponseBody = json.RawMessage(h.piiEngine.RedactFullBody(classified))
		} else {
			redacted, _ := h.piiEngine.RedactForLogging(classified, []byte(body.ResponseBody))
			body.ResponseBody = json.RawMessage(redacted)
		}
	}

	// Request side: only fires for non-GET methods. The engine's
	// RedactRequestBodyForLogging needs the action-specific pattern for
	// messaging POST endpoints; the dashboard does not have access to
	// the original *http.Request, so we reconstruct the lookup using
	// the stored Method+Path. RequestBodyPattern handles this method-aware
	// lookup correctly.
	if body.RequestBody == nil || entry.Method == "" || entry.Method == http.MethodGet {
		return
	}
	synth := &http.Request{Method: entry.Method, URL: mustURL(entry.Path)}
	reqPattern := reg.RequestBodyPattern(synth)
	if reqPattern == "" {
		return
	}
	if redacted, ok := h.piiEngine.RedactRequestBodyForLogging(reqPattern, []byte(body.RequestBody)); ok {
		body.RequestBody = json.RawMessage(redacted)
	} else if reg.IsFullBodyPII(classified, known) {
		// Fail-closed unknown path: same fallback as the async logger.
		body.RequestBody = json.RawMessage(h.piiEngine.RedactFullBody(reqPattern))
	}
}

// mustURL parses a stored path into a minimal *url.URL for use as a synthetic
// request URL when re-running redaction read-side. Stored paths come from
// real requests, so parsing should not fail; if it does we fall back to a
// URL that exercises the same code path safely.
func mustURL(path string) *url.URL {
	u, err := url.Parse(path)
	if err != nil {
		return &url.URL{Path: path}
	}
	return u
}

func (h *Handler) handleMerchants(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("q")
	limit, err := parseInt(r, "limit", 20)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid limit: "+err.Error())
		return
	}

	merchants, err := h.logStore.DistinctMerchants(r.Context(), prefix, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if merchants == nil {
		merchants = []string{}
	}

	writeJSON(w, http.StatusOK, map[string]any{"merchants": merchants})
}
