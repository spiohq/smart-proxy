package pii

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactQueryString_Empty(t *testing.T) {
	assert.Equal(t, "", RedactQueryString("", nil))
}

func TestRedactQueryString_NoMatches(t *testing.T) {
	q := "MarketplaceIds=A1PA6795UKMFR9&CreatedAfter=2025-01-01"
	assert.Equal(t, q, RedactQueryString(q, nil))
}

func TestRedactQueryString_BuyerEmail(t *testing.T) {
	got := RedactQueryString("buyerEmail=foo%40bar.com", nil)
	assert.Equal(t, "buyerEmail=%5BREDACTED%5D", got)
}

func TestRedactQueryString_CaseInsensitive(t *testing.T) {
	got := RedactQueryString("BuyerEmail=a&BUYEREMAIL=b&buyeremail=c", nil)
	assert.Equal(t, "BuyerEmail=%5BREDACTED%5D&BUYEREMAIL=%5BREDACTED%5D&buyeremail=%5BREDACTED%5D", got)
}

func TestRedactQueryString_BuyerName(t *testing.T) {
	got := RedactQueryString("buyerName=John+Doe", nil)
	assert.Equal(t, "buyerName=%5BREDACTED%5D", got)
}

func TestRedactQueryString_MultipleValues(t *testing.T) {
	got := RedactQueryString("buyerEmail=a%40b.com&buyerEmail=c%40d.com", nil)
	assert.Equal(t, "buyerEmail=%5BREDACTED%5D&buyerEmail=%5BREDACTED%5D", got)
}

func TestRedactQueryString_MixedPIIAndNonPII(t *testing.T) {
	got := RedactQueryString("buyerEmail=foo%40bar.com&MarketplaceIds=A1PA6795UKMFR9&dataElements=buyerInfo", nil)
	assert.Equal(t, "buyerEmail=%5BREDACTED%5D&MarketplaceIds=A1PA6795UKMFR9&dataElements=buyerInfo", got)
}

func TestRedactQueryString_PreservesOrder(t *testing.T) {
	got := RedactQueryString("z=1&buyerEmail=foo%40bar.com&a=2", nil)
	assert.Equal(t, "z=1&buyerEmail=%5BREDACTED%5D&a=2", got)
}

func TestRedactQueryString_CustomExtras(t *testing.T) {
	extras := map[string]bool{"customfield": true}
	got := RedactQueryString("CustomField=secret&MarketplaceIds=A1", extras)
	assert.Equal(t, "CustomField=%5BREDACTED%5D&MarketplaceIds=A1", got)
}

func TestRedactQueryString_EmptyValue(t *testing.T) {
	// An empty value for a PII param is still rewritten -- there is nothing
	// to redact, but this keeps the output consistent.
	got := RedactQueryString("buyerEmail=", nil)
	assert.Equal(t, "buyerEmail=%5BREDACTED%5D", got)
}

func TestRedactQueryString_KeyOnly(t *testing.T) {
	// "?buyerEmail" with no '=' should pass through unchanged (malformed,
	// but not our problem).
	got := RedactQueryString("buyerEmail", nil)
	assert.Equal(t, "buyerEmail", got)
}

func TestRedactQueryString_ExtrasMustBeLowercase(t *testing.T) {
	// Documents the contract: callers MUST lowercase extras keys before
	// passing them in. Upper/mixed case is silently ignored. The Registry
	// constructor (NewRegistryWithExtras, added in a later task) will do
	// this lowering once at construction time, so the hot path stays cheap.
	extras := map[string]bool{"CustomField": true} // wrong case on purpose
	got := RedactQueryString("CustomField=secret", extras)
	assert.Equal(t, "CustomField=secret", got, "upper-case extras key must be silently ignored")
}

func TestRedactQueryString_ValueContainsEquals(t *testing.T) {
	// strings.IndexByte returns the FIRST '='. Anything after the first '='
	// is treated as the value, so the entire value (including extra '=')
	// gets replaced.
	got := RedactQueryString("buyerEmail=a=b=c", nil)
	assert.Equal(t, "buyerEmail=%5BREDACTED%5D", got)
}

func TestRedactQueryString_MalformedSeparators(t *testing.T) {
	// Trailing '&', leading '&', and '&&' are passed through faithfully:
	// SP-API does not produce these, but the redactor must not crash on
	// garbage input.
	assert.Equal(t, "buyerEmail=%5BREDACTED%5D&", RedactQueryString("buyerEmail=foo&", nil))
	assert.Equal(t, "&buyerEmail=%5BREDACTED%5D", RedactQueryString("&buyerEmail=foo", nil))
	assert.Equal(t, "a&&buyerEmail=%5BREDACTED%5D", RedactQueryString("a&&buyerEmail=foo", nil))
}

func TestRedactQueryString_EmptyKey(t *testing.T) {
	// "=foo" parses as a key of "" (empty). The empty string is not in the
	// PII map, so the part passes through unchanged.
	got := RedactQueryString("=foo&buyerEmail=bar", nil)
	assert.Equal(t, "=foo&buyerEmail=%5BREDACTED%5D", got)
}
