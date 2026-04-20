package pii

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedactForLogging_RedactMode(t *testing.T) {
	reg := NewRegistry()
	eng := NewEngine(reg)

	body := []byte(`{
		"payload": {
			"Orders": [
				{
					"AmazonOrderId": "123-4567890-1234567",
					"BuyerInfo": {
						"BuyerEmail": "buyer@example.com",
						"BuyerName": "Jane Doe"
					},
					"ShippingAddress": {
						"Name": "Jane Doe",
						"AddressLine1": "123 Main St",
						"City": "Seattle",
						"PostalCode": "98101"
					}
				}
			]
		}
	}`)

	redacted, wasPII := eng.RedactForLogging("/orders/v0/orders", body)
	require.True(t, wasPII)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(redacted, &result))

	orders := result["payload"].(map[string]interface{})["Orders"].([]interface{})
	order := orders[0].(map[string]interface{})

	// Non-PII field must be untouched
	assert.Equal(t, "123-4567890-1234567", order["AmazonOrderId"])

	// PII fields in BuyerInfo must be redacted
	buyerInfo := order["BuyerInfo"].(map[string]interface{})
	assert.Equal(t, "[REDACTED]", buyerInfo["BuyerEmail"])
	assert.Equal(t, "[REDACTED]", buyerInfo["BuyerName"])

	// PII fields in ShippingAddress must be redacted
	addr := order["ShippingAddress"].(map[string]interface{})
	assert.Equal(t, "[REDACTED]", addr["Name"])
	assert.Equal(t, "[REDACTED]", addr["AddressLine1"])
	assert.Equal(t, "[REDACTED]", addr["City"])
	assert.Equal(t, "[REDACTED]", addr["PostalCode"])
}

func TestRedactForLogging_OriginalUnmodified(t *testing.T) {
	reg := NewRegistry()
	eng := NewEngine(reg)

	original := []byte(`{"payload":{"Orders":[{"AmazonOrderId":"111","BuyerInfo":{"BuyerEmail":"test@example.com"}}]}}`)
	// Make a copy to compare against later
	snapshot := make([]byte, len(original))
	copy(snapshot, original)

	redacted, wasPII := eng.RedactForLogging("/orders/v0/orders", original)
	require.True(t, wasPII)

	// Original bytes must be identical to the snapshot
	assert.Equal(t, snapshot, original, "original body bytes must not be modified")

	// Redacted bytes must differ from original
	assert.NotEqual(t, original, redacted)
}

func TestRedactForLogging_OmitMode(t *testing.T) {
	reg := NewRegistry()
	eng := NewEngine(reg)

	body := []byte(`{
		"payload": {
			"Messages": [
				{
					"MessageText": "Hello, please check your order.",
					"Attachments": [{"FileName": "invoice.pdf"}]
				}
			]
		}
	}`)

	redacted, wasPII := eng.RedactForLogging("/messaging/v1/orders/{orderId}/messages", body)
	require.True(t, wasPII)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(redacted, &result))

	messages := result["payload"].(map[string]interface{})["Messages"].([]interface{})
	msg := messages[0].(map[string]interface{})

	// MessageText must be redacted (REDACT mode)
	assert.Equal(t, "[REDACTED]", msg["MessageText"])

	// Attachments must be omitted entirely (OMIT mode)
	_, hasAttachments := msg["Attachments"]
	assert.False(t, hasAttachments, "Attachments key must be removed by OMIT mode")
}

func TestRedactForLogging_HashMode(t *testing.T) {
	// Build a custom registry with HASH mode
	reg := &Registry{
		rules: map[string][]FieldRedaction{
			"/test/hash": {
				{JSONPath: "$.payload.Secret", Mode: RedactModeHash},
			},
		},
		fullBodyEndpoints: map[string]bool{},
	}
	eng := NewEngine(reg)

	secretValue := "my-secret-value"
	body := []byte(fmt.Sprintf(`{"payload":{"Secret":%q}}`, secretValue))

	redacted, wasPII := eng.RedactForLogging("/test/hash", body)
	require.True(t, wasPII)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(redacted, &result))

	hashed, ok := result["payload"].(map[string]interface{})["Secret"].(string)
	require.True(t, ok, "Secret must be a string after hashing")

	// Must start with "sha256:" and have 64 hex characters after the prefix
	assert.True(t, strings.HasPrefix(hashed, "sha256:"), "hash must start with 'sha256:'")
	hexPart := strings.TrimPrefix(hashed, "sha256:")
	assert.Len(t, hexPart, 64, "sha256 hex must be 64 characters")

	// Verify the hash value is correct
	h := sha256.Sum256([]byte(secretValue))
	expected := fmt.Sprintf("sha256:%x", h)
	assert.Equal(t, expected, hashed)
}

func TestRedactForLogging_NoRules_ReturnsFalse(t *testing.T) {
	reg := NewRegistry()
	eng := NewEngine(reg)

	// Catalog endpoint has no PII rules
	body := []byte(`{"payload":{"Items":[{"ASIN":"B001234567","Title":"Some Product"}]}}`)
	result, wasPII := eng.RedactForLogging("/catalog/2022-04-01/items", body)

	assert.False(t, wasPII)
	assert.Equal(t, body, result, "original body must be returned unchanged when no rules match")
}

func TestRedactForLogging_InvalidJSON_ReturnsFalse(t *testing.T) {
	reg := NewRegistry()
	eng := NewEngine(reg)

	invalid := []byte("not valid json")
	result, wasPII := eng.RedactForLogging("/orders/v0/orders", invalid)

	assert.False(t, wasPII)
	assert.Equal(t, invalid, result, "original bytes must be returned when JSON parse fails")
}

func TestRedactFullBody(t *testing.T) {
	reg := NewRegistry()
	eng := NewEngine(reg)

	pattern := "/orders/v0/orders/{orderId}/buyerInfo"
	result := eng.RedactFullBody(pattern)

	var obj map[string]interface{}
	require.NoError(t, json.Unmarshal(result, &obj))

	assert.Equal(t, true, obj["redacted"])
	assert.Equal(t, pattern, obj["endpoint"])
}
