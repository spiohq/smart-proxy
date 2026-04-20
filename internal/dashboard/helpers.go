package dashboard

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// writeJSON marshals data and writes it as a JSON response.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// maxTimeRange is the maximum allowed duration between from and to (90 days).
const maxTimeRange = 90 * 24 * time.Hour

// parseTimeRange extracts "from" and "to" query params as RFC3339 timestamps.
// Returns defaults if not provided: from = defaultFrom, to = now.
// Clamps "to" to the current time and limits the range to maxTimeRange.
func parseTimeRange(r *http.Request, defaultFrom time.Duration) (from, to time.Time, err error) {
	now := time.Now().UTC()
	to = now
	from = now.Add(-defaultFrom)

	if v := r.URL.Query().Get("from"); v != "" {
		from, err = time.Parse(time.RFC3339, v)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		to, err = time.Parse(time.RFC3339, v)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}

	// Clamp "to" so it never exceeds the current time.
	if to.After(now) {
		to = now
	}

	// Ensure "from" is before "to".
	if from.After(to) {
		from = to.Add(-defaultFrom)
	}

	// Cap the range at maxTimeRange.
	if to.Sub(from) > maxTimeRange {
		from = to.Add(-maxTimeRange)
	}

	return from, to, nil
}

// parseInt parses an integer query param, returning defaultVal if not provided.
func parseInt(r *http.Request, key string, defaultVal int) (int, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return defaultVal, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, err
	}
	return n, nil
}
