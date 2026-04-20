package cache_test

import (
	"testing"

	"github.com/spiohq/smart-proxy/internal/cache"
	"github.com/stretchr/testify/assert"
)

func TestGenerateCacheKey_Deterministic(t *testing.T) {
	k1 := cache.GenerateCacheKey("merchant1", "GET", "/orders/v0/orders", "MarketplaceIds=A1&Status=Shipped", "")
	k2 := cache.GenerateCacheKey("merchant1", "GET", "/orders/v0/orders", "MarketplaceIds=A1&Status=Shipped", "")
	assert.Equal(t, k1, k2)
	assert.Equal(t, "merchant1:GET:/orders/v0/orders?MarketplaceIds=A1&Status=Shipped", k1)
}

func TestGenerateCacheKey_QueryParamOrder(t *testing.T) {
	k1 := cache.GenerateCacheKey("m1", "GET", "/orders", "A=1&B=2", "")
	k2 := cache.GenerateCacheKey("m1", "GET", "/orders", "B=2&A=1", "")
	assert.Equal(t, k1, k2, "query param order should not matter")
	assert.Equal(t, "m1:GET:/orders?A=1&B=2", k1)
}

func TestGenerateCacheKey_DifferentMerchants(t *testing.T) {
	k1 := cache.GenerateCacheKey("merchant1", "GET", "/orders", "", "")
	k2 := cache.GenerateCacheKey("merchant2", "GET", "/orders", "", "")
	assert.NotEqual(t, k1, k2)
}

func TestGenerateCacheKey_DifferentMethods(t *testing.T) {
	k1 := cache.GenerateCacheKey("m1", "GET", "/orders", "", "")
	k2 := cache.GenerateCacheKey("m1", "POST", "/orders", "", "")
	assert.NotEqual(t, k1, k2)
}

func TestGenerateCacheKey_CustomSuffix(t *testing.T) {
	k1 := cache.GenerateCacheKey("m1", "GET", "/orders", "", "")
	k2 := cache.GenerateCacheKey("m1", "GET", "/orders", "", "custom-key")
	assert.NotEqual(t, k1, k2)
	assert.Equal(t, "m1:GET:/orders:custom-key", k2)
}

func TestGenerateCacheKey_NoQuery(t *testing.T) {
	k := cache.GenerateCacheKey("m1", "GET", "/orders", "", "")
	assert.Equal(t, "m1:GET:/orders", k)
}

func TestSortQueryParams(t *testing.T) {
	sorted := cache.SortQueryParams("Z=3&A=1&M=2")
	assert.Equal(t, "A=1&M=2&Z=3", sorted)
}

func TestSortQueryParams_Empty(t *testing.T) {
	assert.Equal(t, "", cache.SortQueryParams(""))
}

func TestGenerateCacheKey_DifferentQueryValues(t *testing.T) {
	// Same endpoint, same param name, different values → different cache keys.
	// This is critical: e.g. catalog items with includedData=attributes vs includedData=images
	// must NOT share a cache entry.
	k1 := cache.GenerateCacheKey("m1", "GET", "/catalog/2022-04-01/items", "identifiers=ASIN&identifiersType=ASIN&includedData=attributes", "")
	k2 := cache.GenerateCacheKey("m1", "GET", "/catalog/2022-04-01/items", "identifiers=ASIN&identifiersType=ASIN&includedData=images", "")
	assert.NotEqual(t, k1, k2, "different includedData values must produce different cache keys")
}

func TestGenerateCacheKey_SubsetOfParams(t *testing.T) {
	// Extra query params → different key, even if one is a subset of the other.
	k1 := cache.GenerateCacheKey("m1", "GET", "/catalog/2022-04-01/items", "identifiers=ASIN", "")
	k2 := cache.GenerateCacheKey("m1", "GET", "/catalog/2022-04-01/items", "identifiers=ASIN&includedData=attributes", "")
	assert.NotEqual(t, k1, k2, "subset of params must produce different cache key")
}

func TestSortQueryParams_MultipleValues(t *testing.T) {
	// Multiple values for the same key should be sorted independently.
	sorted := cache.SortQueryParams("color=red&color=blue&size=M")
	assert.Equal(t, "color=blue&color=red&size=M", sorted)
}

func TestSortQueryParams_EncodedValues(t *testing.T) {
	sorted := cache.SortQueryParams("name=hello+world&id=123")
	assert.Equal(t, "id=123&name=hello world", sorted)
}

func TestGenerateCacheKey_SameParamsDifferentOrder(t *testing.T) {
	// Verifies normalization: same params in different order → same key
	k1 := cache.GenerateCacheKey("m1", "GET", "/catalog/2022-04-01/items",
		"includedData=attributes&identifiers=B08N5WRWNW&identifiersType=ASIN&marketplaceIds=ATVPDKIKX0DER", "")
	k2 := cache.GenerateCacheKey("m1", "GET", "/catalog/2022-04-01/items",
		"marketplaceIds=ATVPDKIKX0DER&identifiersType=ASIN&identifiers=B08N5WRWNW&includedData=attributes", "")
	assert.Equal(t, k1, k2, "same params in different order must produce same cache key")
}
