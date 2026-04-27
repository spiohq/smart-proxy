package pii

import (
	"net/http"
	"strings"

	"github.com/spiohq/smart-proxy/internal/endpoint"
)

// RedactionMode controls how a PII field is handled during redaction.
type RedactionMode string

const (
	// RedactModeRedact replaces the field value with "[REDACTED]".
	RedactModeRedact RedactionMode = "REDACT"
	// RedactModeHash replaces the field value with "sha256:<hex>".
	RedactModeHash RedactionMode = "HASH"
	// RedactModeOmit removes the field from the response entirely.
	RedactModeOmit RedactionMode = "OMIT"
)

// FieldRedaction describes a single PII field and how it should be redacted.
type FieldRedaction struct {
	JSONPath string
	Mode     RedactionMode
}

// DefaultPIIRules maps endpoint patterns to their PII field redaction rules.
var DefaultPIIRules = map[string][]FieldRedaction{
	"/orders/v0/orders": {
		{JSONPath: "$.payload.Orders[*].BuyerInfo.BuyerEmail", Mode: RedactModeRedact},
		{JSONPath: "$.payload.Orders[*].BuyerInfo.BuyerName", Mode: RedactModeRedact},
		{JSONPath: "$.payload.Orders[*].BuyerInfo.BuyerCounty", Mode: RedactModeRedact},
		{JSONPath: "$.payload.Orders[*].BuyerInfo.BuyerTaxInfo", Mode: RedactModeRedact},
		{JSONPath: "$.payload.Orders[*].BuyerInfo.PurchaseOrderNumber", Mode: RedactModeRedact},
		{JSONPath: "$.payload.Orders[*].ShippingAddress.Name", Mode: RedactModeRedact},
		{JSONPath: "$.payload.Orders[*].ShippingAddress.AddressLine1", Mode: RedactModeRedact},
		{JSONPath: "$.payload.Orders[*].ShippingAddress.AddressLine2", Mode: RedactModeRedact},
		{JSONPath: "$.payload.Orders[*].ShippingAddress.AddressLine3", Mode: RedactModeRedact},
		{JSONPath: "$.payload.Orders[*].ShippingAddress.City", Mode: RedactModeRedact},
		{JSONPath: "$.payload.Orders[*].ShippingAddress.StateOrRegion", Mode: RedactModeRedact},
		{JSONPath: "$.payload.Orders[*].ShippingAddress.PostalCode", Mode: RedactModeRedact},
		{JSONPath: "$.payload.Orders[*].ShippingAddress.Phone", Mode: RedactModeRedact},
		{JSONPath: "$.payload.Orders[*].ShippingAddress.AddressType", Mode: RedactModeRedact},
		{JSONPath: "$.payload.Orders[*].ShippingAddress.ExtendedFields", Mode: RedactModeRedact},
	},
	"/orders/v0/orders/{orderId}": {
		{JSONPath: "$.payload.BuyerInfo.BuyerEmail", Mode: RedactModeRedact},
		{JSONPath: "$.payload.BuyerInfo.BuyerName", Mode: RedactModeRedact},
		{JSONPath: "$.payload.BuyerInfo.BuyerCounty", Mode: RedactModeRedact},
		{JSONPath: "$.payload.BuyerInfo.BuyerTaxInfo", Mode: RedactModeRedact},
		{JSONPath: "$.payload.BuyerInfo.PurchaseOrderNumber", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShippingAddress.Name", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShippingAddress.AddressLine1", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShippingAddress.AddressLine2", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShippingAddress.AddressLine3", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShippingAddress.City", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShippingAddress.StateOrRegion", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShippingAddress.PostalCode", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShippingAddress.Phone", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShippingAddress.AddressType", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShippingAddress.ExtendedFields", Mode: RedactModeRedact},
	},
	"/orders/v0/orders/{orderId}/orderItems": {
		{JSONPath: "$.payload.OrderItems[*].BuyerInfo.BuyerCustomizedInfo", Mode: RedactModeRedact},
		{JSONPath: "$.payload.OrderItems[*].BuyerInfo.GiftMessageText", Mode: RedactModeRedact},
	},
	"/shipping/v2/shipments": {
		{JSONPath: "$.payload.Shipments[*].ShipTo.Name", Mode: RedactModeRedact},
		{JSONPath: "$.payload.Shipments[*].ShipTo.AddressLine1", Mode: RedactModeRedact},
		{JSONPath: "$.payload.Shipments[*].ShipTo.AddressLine2", Mode: RedactModeRedact},
		{JSONPath: "$.payload.Shipments[*].ShipTo.City", Mode: RedactModeRedact},
		{JSONPath: "$.payload.Shipments[*].ShipTo.StateOrRegion", Mode: RedactModeRedact},
		{JSONPath: "$.payload.Shipments[*].ShipTo.PostalCode", Mode: RedactModeRedact},
		{JSONPath: "$.payload.Shipments[*].ShipTo.Phone", Mode: RedactModeRedact},
		{JSONPath: "$.payload.Shipments[*].ShipTo.Email", Mode: RedactModeRedact},
	},
	"/mfn/v0/shipments": {
		{JSONPath: "$.payload.ShipFromAddress.Name", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShipFromAddress.AddressLine1", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShipFromAddress.City", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShipFromAddress.PostalCode", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShipFromAddress.Phone", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShipFromAddress.Email", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShipToAddress.Name", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShipToAddress.AddressLine1", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShipToAddress.City", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShipToAddress.PostalCode", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShipToAddress.Phone", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShipToAddress.Email", Mode: RedactModeRedact},
	},
	"/fba/outbound/2020-07-01/fulfillmentOrders": {
		{JSONPath: "$.payload.FulfillmentOrders[*].DestinationAddress.Name", Mode: RedactModeRedact},
		{JSONPath: "$.payload.FulfillmentOrders[*].DestinationAddress.Line1", Mode: RedactModeRedact},
		{JSONPath: "$.payload.FulfillmentOrders[*].DestinationAddress.Line2", Mode: RedactModeRedact},
		{JSONPath: "$.payload.FulfillmentOrders[*].DestinationAddress.City", Mode: RedactModeRedact},
		{JSONPath: "$.payload.FulfillmentOrders[*].DestinationAddress.PostalCode", Mode: RedactModeRedact},
		{JSONPath: "$.payload.FulfillmentOrders[*].DestinationAddress.Phone", Mode: RedactModeRedact},
	},
	"/messaging/v1/orders/{orderId}/messages": {
		{JSONPath: "$.payload.Messages[*].MessageText", Mode: RedactModeRedact},
		{JSONPath: "$.payload.Messages[*].Attachments", Mode: RedactModeOmit},
	},
	"/finances/v0/financialEvents": {
		{JSONPath: "$.payload.FinancialEvents.ShipmentEventList[*].AmazonOrderId", Mode: RedactModeRedact},
	},
	"/easyShip/2022-03-23/package": {
		{JSONPath: "$.payload.ScheduledPackageId.AmazonOrderId", Mode: RedactModeRedact},
		{JSONPath: "$.payload.PackageDetails.PackagePickUpSlot.SlotId", Mode: RedactModeRedact},
	},
	"/orders/v0/orders/{orderId}/regulatedInfo": {
		{JSONPath: "$.payload.RegulatedInformation.Fields[*].FieldValue", Mode: RedactModeRedact},
		{JSONPath: "$.payload.BuyerInfo.BuyerEmail", Mode: RedactModeRedact},
		{JSONPath: "$.payload.BuyerInfo.BuyerName", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShippingAddress.Name", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShippingAddress.AddressLine1", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShippingAddress.AddressLine2", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShippingAddress.AddressLine3", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShippingAddress.City", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShippingAddress.StateOrRegion", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShippingAddress.PostalCode", Mode: RedactModeRedact},
		{JSONPath: "$.payload.ShippingAddress.Phone", Mode: RedactModeRedact},
	},
	"/reports/2021-06-30/documents/{documentId}": {
		{JSONPath: "$.url", Mode: RedactModeRedact},
		{JSONPath: "$.encryptionDetails.key", Mode: RedactModeRedact},
	},
	"/feeds/2021-06-30/documents/{feedDocumentId}": {
		{JSONPath: "$.url", Mode: RedactModeRedact},
		{JSONPath: "$.encryptionDetails.key", Mode: RedactModeRedact},
	},
	"/datakiosk/2023-11-15/documents/{documentId}": {
		{JSONPath: "$.documentUrl", Mode: RedactModeRedact},
	},
}

// ConditionalPIIRules maps endpoint patterns to PII field redaction rules
// that only apply when the request contains specific query parameters.
// These endpoints are NOT always PII  -  they only contain PII when the
// caller explicitly requests PII datasets (e.g. ?includedData=BUYER).
// Used for log redaction; cache exclusion is handled by ContainsPII.
var ConditionalPIIRules = map[string][]FieldRedaction{
	// ── Orders API v2026-01-01 ──────────────────────────────────────
	// PII only present when ?includedData contains BUYER or RECIPIENT.
	// The 2026 API has no $.payload wrapper.
	"/orders/2026-01-01/orders": {
		// BUYER fields
		{JSONPath: "$.orders[*].buyer.buyerName", Mode: RedactModeRedact},
		{JSONPath: "$.orders[*].buyer.buyerEmail", Mode: RedactModeRedact},
		{JSONPath: "$.orders[*].buyer.buyerCompanyName", Mode: RedactModeRedact},
		{JSONPath: "$.orders[*].buyer.buyerPurchaseOrderNumber", Mode: RedactModeRedact},
		// RECIPIENT fields
		{JSONPath: "$.orders[*].recipient.deliveryAddress.name", Mode: RedactModeRedact},
		{JSONPath: "$.orders[*].recipient.deliveryAddress.companyName", Mode: RedactModeRedact},
		{JSONPath: "$.orders[*].recipient.deliveryAddress.addressLine1", Mode: RedactModeRedact},
		{JSONPath: "$.orders[*].recipient.deliveryAddress.addressLine2", Mode: RedactModeRedact},
		{JSONPath: "$.orders[*].recipient.deliveryAddress.addressLine3", Mode: RedactModeRedact},
		{JSONPath: "$.orders[*].recipient.deliveryAddress.city", Mode: RedactModeRedact},
		{JSONPath: "$.orders[*].recipient.deliveryAddress.districtOrCounty", Mode: RedactModeRedact},
		{JSONPath: "$.orders[*].recipient.deliveryAddress.stateOrRegion", Mode: RedactModeRedact},
		{JSONPath: "$.orders[*].recipient.deliveryAddress.municipality", Mode: RedactModeRedact},
		{JSONPath: "$.orders[*].recipient.deliveryAddress.postalCode", Mode: RedactModeRedact},
		{JSONPath: "$.orders[*].recipient.deliveryAddress.phone", Mode: RedactModeRedact},
		{JSONPath: "$.orders[*].recipient.deliveryAddress.extendedFields", Mode: RedactModeRedact},
		{JSONPath: "$.orders[*].recipient.deliveryPreference.dropOffLocation", Mode: RedactModeRedact},
		{JSONPath: "$.orders[*].recipient.deliveryPreference.addressInstruction", Mode: RedactModeRedact},
	},
	"/orders/2026-01-01/orders/{orderId}": {
		// BUYER fields
		{JSONPath: "$.buyer.buyerName", Mode: RedactModeRedact},
		{JSONPath: "$.buyer.buyerEmail", Mode: RedactModeRedact},
		{JSONPath: "$.buyer.buyerCompanyName", Mode: RedactModeRedact},
		{JSONPath: "$.buyer.buyerPurchaseOrderNumber", Mode: RedactModeRedact},
		// RECIPIENT fields
		{JSONPath: "$.recipient.deliveryAddress.name", Mode: RedactModeRedact},
		{JSONPath: "$.recipient.deliveryAddress.companyName", Mode: RedactModeRedact},
		{JSONPath: "$.recipient.deliveryAddress.addressLine1", Mode: RedactModeRedact},
		{JSONPath: "$.recipient.deliveryAddress.addressLine2", Mode: RedactModeRedact},
		{JSONPath: "$.recipient.deliveryAddress.addressLine3", Mode: RedactModeRedact},
		{JSONPath: "$.recipient.deliveryAddress.city", Mode: RedactModeRedact},
		{JSONPath: "$.recipient.deliveryAddress.districtOrCounty", Mode: RedactModeRedact},
		{JSONPath: "$.recipient.deliveryAddress.stateOrRegion", Mode: RedactModeRedact},
		{JSONPath: "$.recipient.deliveryAddress.municipality", Mode: RedactModeRedact},
		{JSONPath: "$.recipient.deliveryAddress.postalCode", Mode: RedactModeRedact},
		{JSONPath: "$.recipient.deliveryAddress.phone", Mode: RedactModeRedact},
		{JSONPath: "$.recipient.deliveryAddress.extendedFields", Mode: RedactModeRedact},
		{JSONPath: "$.recipient.deliveryPreference.dropOffLocation", Mode: RedactModeRedact},
		{JSONPath: "$.recipient.deliveryPreference.addressInstruction", Mode: RedactModeRedact},
	},
}

// DefaultFullBodyPIIEndpoints lists endpoint patterns whose entire response body
// is considered PII and should not be cached.
var DefaultFullBodyPIIEndpoints = map[string]bool{
	"/orders/v0/orders/{orderId}/buyerInfo":               true,
	"/orders/v0/orders/{orderId}/address":                 true,
	"/orders/v0/orders/{orderId}/orderItems/buyerInfo":    true,
	"/messaging/v1/orders/{orderId}/messages/{messageId}": true,
}

// Registry maps endpoint patterns to PII field redaction rules.
type Registry struct {
	rules             map[string][]FieldRedaction
	conditionalRules  map[string][]FieldRedaction
	fullBodyEndpoints map[string]bool
	failClosed        bool
	queryParamsExtra  map[string]bool
}

// NewRegistry returns a Registry pre-loaded with the default SP-API PII rules.
func NewRegistry() *Registry {
	return &Registry{
		rules:             DefaultPIIRules,
		conditionalRules:  ConditionalPIIRules,
		fullBodyEndpoints: DefaultFullBodyPIIEndpoints,
	}
}

// NewRegistryFailClosed returns a Registry that treats any path not matching
// a registered SP-API endpoint pattern as PII-bearing. Use this in
// production-style deployments where adding a new SP-API endpoint upstream
// must not silently bypass redaction until the registry is updated.
func NewRegistryFailClosed() *Registry {
	r := NewRegistry()
	r.failClosed = true
	return r
}

// NewRegistryWithExtras returns a Registry with default rules and an
// additional set of query-parameter names to treat as PII (in addition to
// pii.DefaultPIIQueryParams). Names are case-insensitive; they are
// lower-cased internally so callers do not need to pre-process.
func NewRegistryWithExtras(extras []string) *Registry {
	r := NewRegistry()
	if len(extras) > 0 {
		r.queryParamsExtra = make(map[string]bool, len(extras))
		for _, e := range extras {
			e = strings.TrimSpace(strings.ToLower(e))
			if e != "" {
				r.queryParamsExtra[e] = true
			}
		}
	}
	return r
}

// FailClosed reports whether the registry treats unknown endpoints as PII.
func (reg *Registry) FailClosed() bool { return reg.failClosed }

// QueryParamsExtra returns the operator-supplied additional PII query-param
// names (lower-cased). Returns nil for the default registry. The returned
// map is the registry's internal state; callers must treat it as read-only.
func (reg *Registry) QueryParamsExtra() map[string]bool {
	return reg.queryParamsExtra
}

// RulesFor returns the PII field redaction rules for the given endpoint pattern.
// Returns nil (empty slice) if no rules are registered for the pattern.
// This includes both unconditional and conditional rules  -  callers use this
// at log-redaction time, when the response body is already present.
func (reg *Registry) RulesFor(endpointPattern string) []FieldRedaction {
	rules := reg.rules[endpointPattern]
	if cr := reg.conditionalRules[endpointPattern]; len(cr) > 0 {
		rules = append(rules, cr...)
	}
	return rules
}

// IsFullBodyPII reports whether the entire response body for the given endpoint
// pattern is considered PII.
//
// In fail-closed mode, any pattern that is neither in the full-body PII set,
// the unconditional-rule set, nor the conditional-rule set is treated as
// "unknown" and reported as full-body PII. This causes the logger to redact
// the entire body rather than letting an unmapped endpoint leak fields by
// default. Caller passes the *raw* (unclassified) path so we can tell the
// difference between a known pattern and a pass-through path; that detection
// is done via endpoint.ClassifyKnown by the caller before invoking us.
func (reg *Registry) IsFullBodyPII(endpointPattern string) bool {
	if reg.fullBodyEndpoints[endpointPattern] {
		return true
	}
	if !reg.failClosed {
		return false
	}
	if len(reg.rules[endpointPattern]) > 0 {
		return false
	}
	if len(reg.conditionalRules[endpointPattern]) > 0 {
		return false
	}
	// Unmapped pattern in fail-closed mode: treat as full-body PII.
	return true
}

// piiQueryValues maps query parameter names to the values that indicate PII.
// Orders v0 uses "dataElements" with camelCase values; Orders v2026 uses
// "includedData" with uppercase enum values.
var piiQueryValues = map[string]map[string]bool{
	"dataElements": {"buyerInfo": true, "shippingAddress": true},
	"includedData": {"BUYER": true, "RECIPIENT": true},
}

// ContainsPII reports whether the given request may return PII data.
//
// Logic (in order):
//  1. Non-GET requests never return cached PII  -  returns false.
//  2. Classify the request path to a canonical endpoint pattern.
//  3. If the pattern is a full-body PII endpoint  -  returns true.
//  4. If the pattern has registered (unconditional) PII field rules  -  returns true.
//  5. If query params contain PII-bearing values  -  returns true:
//     - "dataElements" with "buyerInfo" or "shippingAddress" (Orders v0)
//     - "includedData" with "BUYER" or "RECIPIENT" (Orders v2026)
//  6. In fail-closed mode, an unrecognized SP-API path  -  returns true.
//  7. Otherwise returns false.
func (reg *Registry) ContainsPII(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}

	pattern, known := endpoint.ClassifyKnown(r.URL.Path)

	if reg.fullBodyEndpoints[pattern] {
		return true
	}

	if len(reg.rules[pattern]) > 0 {
		return true
	}

	// Check query parameters for PII-bearing dataset selectors.
	q := r.URL.Query()
	for paramKey, piiValues := range piiQueryValues {
		for _, val := range q[paramKey] {
			for _, elem := range strings.Split(val, ",") {
				if piiValues[strings.TrimSpace(elem)] {
					return true
				}
			}
		}
	}

	if reg.failClosed && !known {
		return true
	}

	return false
}
