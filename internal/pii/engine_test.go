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

// ── RedactRequestBodyForLogging (F-02) ────────────────────────────────────
// These tests exercise the request-body counterpart of RedactForLogging.
// Rule keys are the schema-verified entries from DefaultRequestBodyPIIRules
// (commit ce6348d): MFN ShipFromAddress, Shipping v1 shipTo+shipFrom,
// Shipping v2 .../rates|directPurchase|oneClickShipment, and the messaging
// POST sub-actions.

func TestRedactRequestBodyForLogging_Messaging(t *testing.T) {
	reg := NewRegistry()
	eng := NewEngine(reg)

	body := []byte(`{"message":{"text":"Hi Maria, see you at Hauptstrasse 42, 10115 Berlin."}}`)
	got, matched := eng.RedactRequestBodyForLogging(
		"/messaging/v1/orders/{orderId}/messages/createConfirmServiceDetails", body)
	require.True(t, matched)
	assert.NotContains(t, string(got), "Maria")
	assert.NotContains(t, string(got), "Hauptstrasse")
	assert.Contains(t, string(got), "REDACTED")
}

func TestRedactRequestBodyForLogging_Mfn_RedactsBuyerOnly(t *testing.T) {
	// MFN createShipment: ShipFromAddress IS the buyer in the return-shipment
	// use case. There is no ShipToAddress field at the request body level
	// (verified against amzn/.../merchantFulfillmentV0.json).
	reg := NewRegistry()
	eng := NewEngine(reg)

	body := []byte(`{
		"ShipmentRequestDetails": {
			"AmazonOrderId": "903-3489051-5871062",
			"ShipFromAddress": {
				"Name": "Real Buyer",
				"AddressLine1": "300 Turnbull Ave",
				"DistrictOrCounty": "Wayne",
				"Email": "buyer@example.com",
				"Phone": "7132341234"
			}
		}
	}`)
	got, matched := eng.RedactRequestBodyForLogging("/mfn/v0/shipments", body)
	require.True(t, matched)
	// Buyer fields redacted.
	assert.NotContains(t, string(got), "Real Buyer")
	assert.NotContains(t, string(got), "buyer@example.com")
	assert.NotContains(t, string(got), "7132341234")
	assert.NotContains(t, string(got), "300 Turnbull Ave")
	assert.NotContains(t, string(got), "Wayne")
	// Order ID is not PII per DPP and stays untouched.
	assert.Contains(t, string(got), "903-3489051-5871062")
}

func TestRedactRequestBodyForLogging_ShippingV2_AllThreeAddressDirections(t *testing.T) {
	// Shipping v2 /rates request: shipTo + shipFrom + returnTo + per-package
	// sellerDisplayName all carry PII per the v2 schema.
	reg := NewRegistry()
	eng := NewEngine(reg)

	body := []byte(`{
		"shipTo":   {"name":"Buyer Bob",  "email":"bob@example.com",  "addressLine1":"100 Buyer St"},
		"shipFrom": {"name":"Seller Sam", "email":"sam@example.com",  "addressLine1":"200 Seller Ave"},
		"returnTo": {"name":"Returns",    "email":"returns@example.com","addressLine1":"300 Return Rd"},
		"packages": [{"sellerDisplayName": "Acme Trading dba John Smith"}]
	}`)
	got, matched := eng.RedactRequestBodyForLogging("/shipping/v2/shipments/rates", body)
	require.True(t, matched)
	// Every direction's name + email redacted.
	for _, leak := range []string{
		"Buyer Bob", "bob@example.com", "100 Buyer St",
		"Seller Sam", "sam@example.com", "200 Seller Ave",
		"Returns", "returns@example.com", "300 Return Rd",
		"Acme Trading dba John Smith",
	} {
		assert.NotContains(t, string(got), leak, "leak %q must be redacted", leak)
	}
}

func TestRedactRequestBodyForLogging_ShippingV1_CopyEmailsArray(t *testing.T) {
	// Shipping v1 shipTo.copyEmails[*] is an array of strings; verify the
	// JSONPath wildcard rule walks into it.
	reg := NewRegistry()
	eng := NewEngine(reg)

	body := []byte(`{
		"shipTo": {
			"name": "Buyer",
			"email": "buyer@example.com",
			"copyEmails": ["cc1@example.com", "cc2@example.com"]
		},
		"shipFrom": {
			"name": "Seller"
		}
	}`)
	got, matched := eng.RedactRequestBodyForLogging("/shipping/v1/shipments", body)
	require.True(t, matched)
	assert.NotContains(t, string(got), "cc1@example.com")
	assert.NotContains(t, string(got), "cc2@example.com")
	assert.NotContains(t, string(got), "buyer@example.com")
	assert.NotContains(t, string(got), "Seller")
}

func TestRedactRequestBodyForLogging_NoRules_ReturnsOriginal(t *testing.T) {
	reg := NewRegistry()
	eng := NewEngine(reg)

	body := []byte(`{"feedType":"POST_PRODUCT_DATA","marketplaceIds":["A1PA6795UKMFR9"]}`)
	got, matched := eng.RedactRequestBodyForLogging("/feeds/2021-06-30/feeds", body)
	assert.False(t, matched)
	assert.Equal(t, body, got, "off-schema endpoints must pass body through untouched")
}

func TestRedactRequestBodyForLogging_InvalidJSON_ReturnsOriginal(t *testing.T) {
	reg := NewRegistry()
	eng := NewEngine(reg)

	body := []byte(`not json at all`)
	got, matched := eng.RedactRequestBodyForLogging(
		"/messaging/v1/orders/{orderId}/messages/createConfirmServiceDetails", body)
	assert.False(t, matched)
	assert.Equal(t, body, got)
}

func TestRedactRequestBodyForLogging_FullBodySentinel(t *testing.T) {
	// A "$" rule is the full-body marker; the engine returns a placeholder
	// document instead of a field-walked redaction. Today no entry in
	// DefaultRequestBodyPIIRules uses this (EasyShip bulk was removed in
	// commit ce6348d), but the engine must still honor the contract because
	// the fail-closed path in RequestBodyPattern returns a raw path that the
	// caller will treat as full-body.
	reg := &Registry{
		requestBodyRules: map[string][]FieldRedaction{
			"/x/full-body": {{JSONPath: "$", Mode: RedactModeRedact}},
		},
	}
	eng := NewEngine(reg)

	body := []byte(`{"buyerEmail":"victim@example.com","buyerName":"Foo"}`)
	got, matched := eng.RedactRequestBodyForLogging("/x/full-body", body)
	require.True(t, matched)
	assert.NotContains(t, string(got), "victim@example.com")
	assert.NotContains(t, string(got), "Foo")
	// Placeholder format (RedactFullBody): {"redacted":true,"endpoint":"..."}
	var obj map[string]interface{}
	require.NoError(t, json.Unmarshal(got, &obj))
	assert.Equal(t, true, obj["redacted"])
	assert.Equal(t, "/x/full-body", obj["endpoint"])
}
