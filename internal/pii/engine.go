package pii

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// Engine applies PII redaction rules to response bodies using the Registry.
type Engine struct {
	registry *Registry
}

// NewEngine creates a new Engine backed by the given Registry.
func NewEngine(registry *Registry) *Engine {
	return &Engine{registry: registry}
}

// Registry returns the underlying registry.
func (e *Engine) Registry() *Registry {
	return e.registry
}

// RedactForLogging creates a redacted copy of the response body.
// The original bytes are never modified.
// Returns (redacted copy, true) if any PII rules matched,
// or (original body, false) if no rules matched or the body is not valid JSON.
func (e *Engine) RedactForLogging(endpoint string, body []byte) ([]byte, bool) {
	rules := e.registry.RulesFor(endpoint)
	if len(rules) == 0 {
		return body, false
	}

	var data interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return body, false
	}

	anyMatched := false
	for _, rule := range rules {
		mode := rule.Mode
		matched := WalkAndApply(data, rule.JSONPath, func(parent map[string]interface{}, key string) bool {
			switch mode {
			case RedactModeRedact:
				parent[key] = "[REDACTED]"
			case RedactModeHash:
				parent[key] = hashValue(parent[key])
			case RedactModeOmit:
				delete(parent, key)
			}
			return true
		})
		if matched {
			anyMatched = true
		}
	}

	if !anyMatched {
		return body, false
	}

	redacted, err := json.Marshal(data)
	if err != nil {
		return body, false
	}
	return redacted, true
}

// RedactFullBody returns a placeholder JSON object for full-body PII endpoints.
// The returned bytes represent: {"redacted": true, "endpoint": "<pattern>"}
func (e *Engine) RedactFullBody(endpoint string) []byte {
	b, _ := json.Marshal(map[string]interface{}{
		"redacted": true,
		"endpoint": endpoint,
	})
	return b
}

// hashValue returns "sha256:<hex>" for the given value.
// String values are hashed directly; all other values are JSON-serialized first.
func hashValue(v interface{}) string {
	var s string
	switch val := v.(type) {
	case string:
		s = val
	default:
		b, _ := json.Marshal(val)
		s = string(b)
	}
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("sha256:%x", h)
}
