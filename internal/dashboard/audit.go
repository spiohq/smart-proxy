package dashboard

import (
	"net/http"
	"time"

	"github.com/spiohq/smart-proxy/internal/audit"
)

func (h *Handler) handleAudit(w http.ResponseWriter, r *http.Request) {
	from, to, err := parseTimeRange(r, 24*time.Hour)
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

	filter := audit.AuditFilter{
		From:      from,
		To:        to,
		EventType: r.URL.Query().Get("eventType"),
		Limit:     limit,
		Offset:    offset,
	}

	rows, total, err := h.auditStore.QueryAuditEvents(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"rows": rows, "total": total})
}
