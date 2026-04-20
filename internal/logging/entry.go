package logging

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/spiohq/smart-proxy/internal/bodies"
	"github.com/spiohq/smart-proxy/internal/storage"
)

// LogEntry carries both metadata and body through the async logging pipeline.
type LogEntry struct {
	Meta *storage.RequestLog
	Body *bodies.BodyEntry
}

// GenerateRequestID produces a 32-character hex string (16 random bytes).
func GenerateRequestID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
