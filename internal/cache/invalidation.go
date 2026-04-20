package cache

import (
	"context"
	"net/http"
	"strings"
)

// InvalidateOnMutation invalidates cached entries when a write operation occurs.
// Uses prefix-based invalidation: strips the trailing ID segment from the path
// and deletes all cache entries whose key starts with "merchantKey:GET:" + resourcePrefix.
// Also invalidates batch-cached listing offer elements when a listing SKU is mutated.
func InvalidateOnMutation(c Cache, merchantKey, method, path string) {
	if method == http.MethodGet {
		return
	}

	resourcePrefix := ExtractResourcePrefix(path)
	prefix := merchantKey + ":GET:" + resourcePrefix
	c.DeleteByPrefix(context.Background(), prefix)

	// Invalidate batch-cached listing offers when a listing SKU is mutated.
	// PUT/PATCH /listings/2021-08-01/items/{sellerId}/{sku} should invalidate
	// cached batch elements for that SKU.
	if method == http.MethodPut || method == http.MethodPatch {
		invalidateBatchListingOffers(c, merchantKey, path)
	}
}

// invalidateBatchListingOffers checks if the mutated path is a listing item
// and invalidates any batch-cached listing offer elements for that SKU.
// Path format: /listings/2021-08-01/items/{sellerId}/{sku}
func invalidateBatchListingOffers(c Cache, merchantKey, path string) {
	const listingsPrefix = "/listings/"
	const itemsSegment = "/items/"
	if !strings.HasPrefix(path, listingsPrefix) {
		return
	}
	idx := strings.Index(path, itemsSegment)
	if idx < 0 {
		return
	}
	// After /items/ we expect {sellerId}/{sku}
	rest := path[idx+len(itemsSegment):]
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) < 2 {
		return
	}
	sku := strings.TrimRight(parts[1], "/")
	if sku == "" {
		return
	}

	// Delete batch-cached listing offers for this SKU across all batch paths.
	// Key format: merchantKey:BATCH:/batches/products/pricing/v0/listingOffers:listingOffers:SKU:...
	batchPrefix := merchantKey + ":BATCH:/batches/products/pricing/v0/listingOffers:listingOffers:" + sku + ":"
	c.DeleteByPrefix(context.Background(), batchPrefix)
}

// ExtractResourcePrefix strips the trailing path segment (assumed to be an ID)
// to find the broader cacheable resource path.
// "/listings/2021-08-01/items/SELLER/SKU123" → "/listings/2021-08-01/items/SELLER"
// "/orders/v0/orders/123-456-789" → "/orders/v0/orders"
// "/orders/v0/orders" → "/orders/v0/orders" (no trailing ID to strip)
func ExtractResourcePrefix(path string) string {
	path = strings.TrimRight(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) <= 4 {
		return path
	}
	return strings.Join(parts[:len(parts)-1], "/")
}
