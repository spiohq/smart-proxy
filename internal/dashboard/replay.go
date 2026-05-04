package dashboard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"time"

	"github.com/spiohq/smart-proxy/internal/storage"
)

type replayResponse struct {
	Available       bool              `json:"available"`
	Reason          string            `json:"reason,omitempty"`
	StatusCode      int               `json:"statusCode,omitempty"`
	ResponseHeaders map[string]string `json:"responseHeaders,omitempty"`
	ResponseBody    json.RawMessage   `json:"responseBody,omitempty"`
	ReplayError     string            `json:"replayError,omitempty"`
	BodyUnavailable bool              `json:"bodyUnavailable,omitempty"`
}

func (h *Handler) handleReplay(w http.ResponseWriter, r *http.Request) {
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

	proxyHandler := h.handlerForRegion(entry.Region)
	if h.tokenStore == nil || proxyHandler == nil {
		writeJSON(w, http.StatusOK, replayResponse{
			Available: false,
			Reason:    "Replay is not available: proxy not fully configured.",
		})
		return
	}

	token, ok := h.tokenStore.Get(entry.MerchantKey)
	if !ok {
		writeJSON(w, http.StatusOK, replayResponse{
			Available: false,
			Reason:    h.tokenStore.UnavailabilityReason(entry.MerchantKey),
		})
		return
	}

	reqBody, bodyUnavailable := h.loadReplayBody(r, entry)

	replayReq, err := buildReplayRequest(r, entry, token, reqBody)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	rec := httptest.NewRecorder()
	proxyHandler.ServeHTTP(rec, replayReq)

	writeJSON(w, http.StatusOK, collectReplayResponse(rec.Result(), bodyUnavailable))
}

// loadReplayBody fetches the stored request body for the entry. Returns
// (nil, true) when the body was evicted and the method is mutating.
func (h *Handler) loadReplayBody(r *http.Request, entry *storage.RequestLog) ([]byte, bool) {
	if entry.BodyFile != "" {
		if stored, err := h.bodyStore.Read(r.Context(), entry.BodyFile, entry.BodyOffset, entry.BodyLength); err == nil && stored.RequestBody != nil {
			return []byte(stored.RequestBody), false
		}
	}
	mutating := entry.Method != http.MethodGet && entry.Method != http.MethodHead
	return nil, mutating && entry.BodyFile == ""
}

// buildReplayRequest constructs the outbound *http.Request for the replay.
func buildReplayRequest(r *http.Request, entry *storage.RequestLog, token string, reqBody []byte) (*http.Request, error) {
	rawPath := entry.Path
	if entry.QueryParams != "" {
		rawPath = rawPath + "?" + entry.QueryParams
	}
	u, err := url.Parse(rawPath)
	if err != nil {
		return nil, fmt.Errorf("invalid stored path")
	}

	var bodyReader io.Reader
	if len(reqBody) > 0 {
		bodyReader = bytes.NewReader(reqBody)
	}

	req, err := http.NewRequestWithContext(r.Context(), entry.Method, u.String(), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to build replay request")
	}
	req.URL = u
	req.Header.Set("X-Amz-Access-Token", token)
	req.Header.Set("X-Amz-Date", time.Now().UTC().Format("20060102T150405Z"))
	req.Header.Set("X-SP-Proxy-Merchant-Id", entry.MerchantKey)
	req.Header.Set("X-SP-Proxy-No-Cache", "true")
	if entry.Region != "" {
		req.Header.Set("X-SP-Proxy-Region", entry.Region)
	}
	return req, nil
}

// handlerForRegion returns the proxy handler for the given region string.
// Falls back to proxyHandler when no region-specific handler is registered.
func (h *Handler) handlerForRegion(region string) http.Handler {
	if h.regionHandlers != nil {
		if ph, ok := h.regionHandlers[region]; ok {
			return ph
		}
	}
	return h.proxyHandler
}

// collectReplayResponse reads the recorder result and builds the response DTO.
func collectReplayResponse(result *http.Response, bodyUnavailable bool) replayResponse {
	respHeaders := make(map[string]string, len(result.Header))
	for k, vals := range result.Header {
		if len(vals) > 0 {
			respHeaders[k] = vals[0]
		}
	}

	respBodyBytes, _ := io.ReadAll(result.Body)
	result.Body.Close()

	var respBodyJSON json.RawMessage
	if json.Valid(respBodyBytes) {
		respBodyJSON = json.RawMessage(respBodyBytes)
	}

	replayErr := ""
	if result.StatusCode == http.StatusUnauthorized {
		replayErr = "Token rejected by Amazon (401). The token may have expired. Send a new request through your client to refresh it."
	}

	return replayResponse{
		Available:       true,
		StatusCode:      result.StatusCode,
		ResponseHeaders: respHeaders,
		ResponseBody:    respBodyJSON,
		ReplayError:     replayErr,
		BodyUnavailable: bodyUnavailable,
	}
}
