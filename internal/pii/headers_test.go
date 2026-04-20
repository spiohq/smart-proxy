package pii

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactHeaders_RedactsSensitive(t *testing.T) {
	headers := http.Header{
		"X-Amz-Access-Token":   {"secret-token"},
		"Authorization":        {"Bearer my-secret"},
		"X-Amz-Security-Token": {"session-token"},
		"Content-Type":         {"application/json"},
		"X-Custom":             {"custom-value"},
	}

	redacted := RedactHeaders(headers)

	assert.Equal(t, []string{"[REDACTED]"}, redacted["X-Amz-Access-Token"])
	assert.Equal(t, []string{"[REDACTED]"}, redacted["Authorization"])
	assert.Equal(t, []string{"[REDACTED]"}, redacted["X-Amz-Security-Token"])
	assert.Equal(t, []string{"application/json"}, redacted["Content-Type"])
	assert.Equal(t, []string{"custom-value"}, redacted["X-Custom"])
}

func TestRedactHeaders_OriginalUnmodified(t *testing.T) {
	headers := http.Header{
		"Authorization": {"Bearer original"},
		"Content-Type":  {"application/json"},
	}

	RedactHeaders(headers)

	assert.Equal(t, []string{"Bearer original"}, headers["Authorization"])
	assert.Equal(t, []string{"application/json"}, headers["Content-Type"])
}

func TestRedactHeaders_CaseInsensitive(t *testing.T) {
	headers := http.Header{
		"authorization": {"Bearer lowercase"},
	}

	redacted := RedactHeaders(headers)

	assert.Equal(t, []string{"[REDACTED]"}, redacted["authorization"])
}

func TestRedactHeaders_Empty(t *testing.T) {
	headers := http.Header{}

	redacted := RedactHeaders(headers)

	assert.NotNil(t, redacted)
	assert.Empty(t, redacted)
}
