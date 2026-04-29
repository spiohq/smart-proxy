package pii

import (
	"net/http"
	"strings"
)

// SensitiveHeaders enumerates header names whose values must be redacted in
// logs regardless of direction (request or response). Keys are lower-case
// and matched case-insensitively. Set-Cookie sits here as defense-in-depth
// for the response side: Amazon's SP-API does not normally emit it, but if
// it ever does, session-bearing values must not land in JSONL.
//
// Pentest finding F-12.
var SensitiveHeaders = map[string]bool{
	"x-amz-access-token":   true,
	"authorization":        true,
	"x-amz-security-token": true,
	"set-cookie":           true,
	"cookie":               true,
}

// RedactHeaders returns a copy of headers with sensitive values replaced by "[REDACTED]".
func RedactHeaders(headers http.Header) http.Header {
	copy := make(http.Header, len(headers))
	for k, vals := range headers {
		if SensitiveHeaders[strings.ToLower(k)] {
			copy[k] = []string{"[REDACTED]"}
		} else {
			vCopy := make([]string, len(vals))
			for i, v := range vals {
				vCopy[i] = v
			}
			copy[k] = vCopy
		}
	}
	return copy
}
