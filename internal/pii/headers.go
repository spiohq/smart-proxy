package pii

import (
	"net/http"
	"strings"
)

var SensitiveHeaders = map[string]bool{
	"x-amz-access-token":   true,
	"authorization":        true,
	"x-amz-security-token": true,
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
