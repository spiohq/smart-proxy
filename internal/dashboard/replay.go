package dashboard

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"time"
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

	if h.tokenStore == nil {
		writeJSON(w, http.StatusOK, replayResponse{
			Available: false,
			Reason:    "Replay is not available: token store not configured.",
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

	if h.proxyHandler == nil {
		writeJSON(w, http.StatusOK, replayResponse{
			Available: false,
			Reason:    "Replay is not available: proxy handler not configured.",
		})
		return
	}

	var reqBody []byte
	bodyUnavailable := false
	if entry.BodyFile != "" {
		if stored, berr := h.bodyStore.Read(r.Context(), entry.BodyFile, entry.BodyOffset, entry.BodyLength); berr == nil && stored.RequestBody != nil {
			reqBody = []byte(stored.RequestBody)
		}
	}
	if entry.Method != http.MethodGet && entry.Method != http.MethodHead && entry.BodyFile == "" {
		bodyUnavailable = true
	}

	rawPath := entry.Path
	if entry.QueryParams != "" {
		rawPath = rawPath + "?" + entry.QueryParams
	}
	u, err := url.Parse(rawPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid stored path")
		return
	}

	var bodyReader io.Reader
	if len(reqBody) > 0 {
		bodyReader = bytes.NewReader(reqBody)
	}

	replayReq, err := http.NewRequestWithContext(r.Context(), entry.Method, u.String(), bodyReader)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build replay request")
		return
	}
	replayReq.URL = u
	replayReq.Header.Set("X-Amz-Access-Token", token)
	replayReq.Header.Set("X-Amz-Date", time.Now().UTC().Format("20060102T150405Z"))
	replayReq.Header.Set("X-SP-Proxy-Merchant-Id", entry.MerchantKey)
	replayReq.Header.Set("X-SP-Proxy-No-Cache", "true")
	if entry.Region != "" {
		replayReq.Header.Set("X-SP-Proxy-Region", entry.Region)
	}

	rec := httptest.NewRecorder()
	h.proxyHandler.ServeHTTP(rec, replayReq)
	result := rec.Result()

	respHeaders := make(map[string]string)
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

	writeJSON(w, http.StatusOK, replayResponse{
		Available:       true,
		StatusCode:      result.StatusCode,
		ResponseHeaders: respHeaders,
		ResponseBody:    respBodyJSON,
		ReplayError:     replayErr,
		BodyUnavailable: bodyUnavailable,
	})
}
