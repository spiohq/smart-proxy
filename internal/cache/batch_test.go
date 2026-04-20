package cache_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spiohq/smart-proxy/internal/cache"
	"github.com/spiohq/smart-proxy/internal/config"
	"github.com/spiohq/smart-proxy/internal/merchant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -- Helper: build a batch handler with a counting upstream --

func batchHandler(t *testing.T, respBody string, callCount *int) http.Handler {
	t.Helper()
	mc := cache.NewMemoryCache(1 << 20)
	t.Cleanup(func() { mc.Close() })
	tc := cache.NewTierClassifier(nil)
	cfg := &config.CacheConfig{
		Enabled:    true,
		MaxMemory:  1 << 20,
		DefaultTTL: "60s",
	}

	up := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(respBody))
	})

	return addMerchantCtx(cache.CacheMiddleware(mc, tc, cfg)(up))
}

func batchHandlerWithCache(t *testing.T, respBody string, callCount *int) (http.Handler, *cache.MemoryCache) {
	t.Helper()
	mc := cache.NewMemoryCache(1 << 20)
	t.Cleanup(func() { mc.Close() })
	tc := cache.NewTierClassifier(nil)
	cfg := &config.CacheConfig{
		Enabled:    true,
		MaxMemory:  1 << 20,
		DefaultTTL: "60s",
	}

	up := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(respBody))
	})

	return addMerchantCtx(cache.CacheMiddleware(mc, tc, cfg)(up)), mc
}

// -- Item Offers Batch --

func itemOffersBatchRequest(asins ...string) string {
	type req struct {
		URI           string `json:"uri"`
		Method        string `json:"method"`
		MarketplaceId string `json:"MarketplaceId"`
		ItemCondition string `json:"ItemCondition"`
		CustomerType  string `json:"CustomerType"`
	}
	reqs := make([]req, len(asins))
	for i, asin := range asins {
		reqs[i] = req{
			URI:           "/products/pricing/v0/items/" + asin + "/offers",
			Method:        "GET",
			MarketplaceId: "ATVPDKIKX0DER",
			ItemCondition: "New",
			CustomerType:  "Consumer",
		}
	}
	wrapper := map[string]any{"requests": reqs}
	b, _ := json.Marshal(wrapper)
	return string(b)
}

func itemOffersBatchResponse(asins ...string) string {
	type status struct {
		StatusCode   int    `json:"statusCode"`
		ReasonPhrase string `json:"reasonPhrase"`
	}
	type respElem struct {
		Status status `json:"status"`
		Body   any    `json:"body"`
	}
	elems := make([]respElem, len(asins))
	for i, asin := range asins {
		elems[i] = respElem{
			Status: status{StatusCode: 200, ReasonPhrase: "OK"},
			Body:   map[string]any{"payload": map[string]any{"ASIN": asin, "status": "Success"}},
		}
	}
	wrapper := map[string]any{"responses": elems}
	b, _ := json.Marshal(wrapper)
	return string(b)
}

func TestBatchCache_ItemOffers_MissAndHit(t *testing.T) {
	resp := itemOffersBatchResponse("B000AAA", "B000BBB")
	callCount := 0
	handler := batchHandler(t, resp, &callCount)

	body := itemOffersBatchRequest("B000AAA", "B000BBB")

	// First request: MISS
	req := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "MISS", w.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)

	// Second request (same elements): HIT
	req2 := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, 200, w2.Code)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount, "upstream should not be called on batch HIT")
}

func TestBatchCache_ItemOffers_OrderIndependent(t *testing.T) {
	resp := itemOffersBatchResponse("B000AAA", "B000BBB")
	callCount := 0
	handler := batchHandler(t, resp, &callCount)

	// Request with [A, B]
	body1 := itemOffersBatchRequest("B000AAA", "B000BBB")
	req := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body1))
	handler.ServeHTTP(httptest.NewRecorder(), req)
	assert.Equal(t, 1, callCount)

	// Request with [B, A] - reversed order, should still HIT
	body2 := itemOffersBatchRequest("B000BBB", "B000AAA")
	req2 := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body2))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount, "reversed order should be a cache HIT")
}

func TestBatchCache_ItemOffers_CrossBatchHit(t *testing.T) {
	// Batch 1: cache A, B, C individually
	resp1 := itemOffersBatchResponse("B000AAA", "B000BBB", "B000CCC")
	callCount := 0
	handler := batchHandler(t, resp1, &callCount)

	body1 := itemOffersBatchRequest("B000AAA", "B000BBB", "B000CCC")
	req1 := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body1))
	handler.ServeHTTP(httptest.NewRecorder(), req1)
	assert.Equal(t, 1, callCount)

	// Batch 2: request A, B, C in a different combination - should HIT
	body2 := itemOffersBatchRequest("B000CCC", "B000AAA", "B000BBB")
	req2 := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body2))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)
}

func TestBatchCache_ItemOffers_PartialMiss(t *testing.T) {
	// Cache A and B
	resp1 := itemOffersBatchResponse("B000AAA", "B000BBB")
	callCount := 0
	handler := batchHandler(t, resp1, &callCount)

	body1 := itemOffersBatchRequest("B000AAA", "B000BBB")
	req1 := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body1))
	handler.ServeHTTP(httptest.NewRecorder(), req1)
	assert.Equal(t, 1, callCount)

	// Request A, B, D - D is not cached, so MISS (full request goes upstream)
	body2 := itemOffersBatchRequest("B000AAA", "B000BBB", "B000DDD")
	req2 := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body2))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "MISS", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 2, callCount, "partial miss should call upstream")
}

// -- Listing Offers Batch --

func listingOffersBatchRequest(skus ...string) string {
	type req struct {
		URI           string `json:"uri"`
		Method        string `json:"method"`
		MarketplaceId string `json:"MarketplaceId"`
		ItemCondition string `json:"ItemCondition"`
	}
	reqs := make([]req, len(skus))
	for i, sku := range skus {
		reqs[i] = req{
			URI:           "/products/pricing/v0/listings/" + sku + "/offers",
			Method:        "GET",
			MarketplaceId: "ATVPDKIKX0DER",
			ItemCondition: "New",
		}
	}
	wrapper := map[string]any{"requests": reqs}
	b, _ := json.Marshal(wrapper)
	return string(b)
}

func listingOffersBatchResponse(skus ...string) string {
	type status struct {
		StatusCode   int    `json:"statusCode"`
		ReasonPhrase string `json:"reasonPhrase"`
	}
	type respElem struct {
		Status status `json:"status"`
		Body   any    `json:"body"`
	}
	elems := make([]respElem, len(skus))
	for i, sku := range skus {
		elems[i] = respElem{
			Status: status{StatusCode: 200, ReasonPhrase: "OK"},
			Body:   map[string]any{"payload": map[string]any{"SKU": sku, "status": "Success"}},
		}
	}
	wrapper := map[string]any{"responses": elems}
	b, _ := json.Marshal(wrapper)
	return string(b)
}

func TestBatchCache_ListingOffers_MissAndHit(t *testing.T) {
	resp := listingOffersBatchResponse("SKU-1", "SKU-2")
	callCount := 0
	handler := batchHandler(t, resp, &callCount)

	body := listingOffersBatchRequest("SKU-1", "SKU-2")

	// MISS
	req := httptest.NewRequest("POST", "/batches/products/pricing/v0/listingOffers", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, "MISS", w.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)

	// HIT
	req2 := httptest.NewRequest("POST", "/batches/products/pricing/v0/listingOffers", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)
}

func TestBatchCache_ListingOffers_InvalidateOnPut(t *testing.T) {
	resp := listingOffersBatchResponse("SKU-1", "SKU-2")
	callCount := 0
	handler, mc := batchHandlerWithCache(t, resp, &callCount)

	// Warm cache
	body := listingOffersBatchRequest("SKU-1", "SKU-2")
	req := httptest.NewRequest("POST", "/batches/products/pricing/v0/listingOffers", bytes.NewBufferString(body))
	handler.ServeHTTP(httptest.NewRecorder(), req)
	assert.Equal(t, 1, callCount)

	// Verify HIT
	req2 := httptest.NewRequest("POST", "/batches/products/pricing/v0/listingOffers", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))

	// PUT on SKU-1 listing should invalidate
	cache.InvalidateOnMutation(mc, "test-merchant", http.MethodPut, "/listings/2021-08-01/items/SELLER123/SKU-1")

	// Now batch with SKU-1 should MISS
	req3 := httptest.NewRequest("POST", "/batches/products/pricing/v0/listingOffers", bytes.NewBufferString(body))
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req3)
	assert.Equal(t, "MISS", w3.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 2, callCount, "should call upstream after SKU-1 invalidation")
}

func TestBatchCache_ListingOffers_InvalidateOnPatch(t *testing.T) {
	resp := listingOffersBatchResponse("SKU-X")
	callCount := 0
	handler, mc := batchHandlerWithCache(t, resp, &callCount)

	body := listingOffersBatchRequest("SKU-X")
	req := httptest.NewRequest("POST", "/batches/products/pricing/v0/listingOffers", bytes.NewBufferString(body))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	// Verify HIT
	req2 := httptest.NewRequest("POST", "/batches/products/pricing/v0/listingOffers", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))

	// PATCH on SKU-X
	cache.InvalidateOnMutation(mc, "test-merchant", http.MethodPatch, "/listings/2021-08-01/items/SELLER/SKU-X")

	// Should MISS now
	req3 := httptest.NewRequest("POST", "/batches/products/pricing/v0/listingOffers", bytes.NewBufferString(body))
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req3)
	assert.Equal(t, "MISS", w3.Header().Get("X-SP-Proxy-Cache"))
}

// -- Fees Batch (bare array format) --

func feesBatchRequest(ids ...string) string {
	type listingPrice struct {
		CurrencyCode string  `json:"CurrencyCode"`
		Amount       float64 `json:"Amount"`
	}
	type priceToEstimate struct {
		ListingPrice listingPrice `json:"ListingPrice"`
	}
	type fer struct {
		MarketplaceId     string         `json:"MarketplaceId"`
		IsAmazonFulfilled bool           `json:"IsAmazonFulfilled"`
		PriceToEstimateFees priceToEstimate `json:"PriceToEstimateFees"`
		Identifier        string         `json:"Identifier"`
	}
	type elem struct {
		IdType           string `json:"IdType"`
		IdValue          string `json:"IdValue"`
		FeesEstimateRequest fer  `json:"FeesEstimateRequest"`
	}

	elements := make([]elem, len(ids))
	for i, id := range ids {
		elements[i] = elem{
			IdType:  "ASIN",
			IdValue: id,
			FeesEstimateRequest: fer{
				MarketplaceId:     "ATVPDKIKX0DER",
				IsAmazonFulfilled: true,
				PriceToEstimateFees: priceToEstimate{
					ListingPrice: listingPrice{CurrencyCode: "USD", Amount: 25.99},
				},
				Identifier: "req-" + id,
			},
		}
	}
	b, _ := json.Marshal(elements)
	return string(b)
}

func feesBatchResponse(ids ...string) string {
	type respElem struct {
		Status string `json:"Status"`
		FeesEstimateIdentifier struct {
			IdValue string `json:"IdValue"`
		} `json:"FeesEstimateIdentifier"`
	}
	elems := make([]respElem, len(ids))
	for i, id := range ids {
		elems[i] = respElem{Status: "Success"}
		elems[i].FeesEstimateIdentifier.IdValue = id
	}
	b, _ := json.Marshal(elems)
	return string(b)
}

func TestBatchCache_Fees_MissAndHit(t *testing.T) {
	resp := feesBatchResponse("B07XJ8C8F5", "B08N5WRWNW")
	callCount := 0
	handler := batchHandler(t, resp, &callCount)

	body := feesBatchRequest("B07XJ8C8F5", "B08N5WRWNW")

	// MISS
	req := httptest.NewRequest("POST", "/products/fees/v0/feesEstimate", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, "MISS", w.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)

	// HIT
	req2 := httptest.NewRequest("POST", "/products/fees/v0/feesEstimate", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)
}

func TestBatchCache_Fees_OrderIndependent(t *testing.T) {
	resp := feesBatchResponse("ASIN-A", "ASIN-B")
	callCount := 0
	handler := batchHandler(t, resp, &callCount)

	body1 := feesBatchRequest("ASIN-A", "ASIN-B")
	req := httptest.NewRequest("POST", "/products/fees/v0/feesEstimate", bytes.NewBufferString(body1))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	// Reversed order
	body2 := feesBatchRequest("ASIN-B", "ASIN-A")
	req2 := httptest.NewRequest("POST", "/products/fees/v0/feesEstimate", bytes.NewBufferString(body2))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)
}

func TestBatchCache_Fees_CrossBatchHit(t *testing.T) {
	// Cache A, B in first batch
	resp1 := feesBatchResponse("ASIN-A", "ASIN-B")
	callCount := 0
	handler := batchHandler(t, resp1, &callCount)

	body1 := feesBatchRequest("ASIN-A", "ASIN-B")
	req1 := httptest.NewRequest("POST", "/products/fees/v0/feesEstimate", bytes.NewBufferString(body1))
	handler.ServeHTTP(httptest.NewRecorder(), req1)
	assert.Equal(t, 1, callCount)

	// Request just A - should HIT from cached elements
	bodyA := feesBatchRequest("ASIN-A")
	reqA := httptest.NewRequest("POST", "/products/fees/v0/feesEstimate", bytes.NewBufferString(bodyA))
	wA := httptest.NewRecorder()
	handler.ServeHTTP(wA, reqA)
	assert.Equal(t, "HIT", wA.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount, "subset of cached elements should HIT")
}

// -- Bypass / disabled --

func TestBatchCache_Bypass(t *testing.T) {
	resp := itemOffersBatchResponse("B000AAA")
	callCount := 0
	handler := batchHandler(t, resp, &callCount)

	body := itemOffersBatchRequest("B000AAA")

	// Warm cache
	req := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	// Bypass
	req2 := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body))
	req2.Header.Set("X-SP-Proxy-No-Cache", "true")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "BYPASS", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 2, callCount)
}

func TestBatchCache_ResponseBodyFormat_Wrapped(t *testing.T) {
	resp := itemOffersBatchResponse("B000AAA", "B000BBB")
	callCount := 0
	handler := batchHandler(t, resp, &callCount)

	body := itemOffersBatchRequest("B000AAA", "B000BBB")

	// Warm cache
	req := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	// HIT - verify response structure is wrapped
	req2 := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	respBody, _ := io.ReadAll(w2.Body)
	var parsed map[string]json.RawMessage
	err := json.Unmarshal(respBody, &parsed)
	require.NoError(t, err)
	_, hasResponses := parsed["responses"]
	assert.True(t, hasResponses, "cached batch response should have 'responses' wrapper")
}

func TestBatchCache_ResponseBodyFormat_Bare(t *testing.T) {
	resp := feesBatchResponse("ASIN-A")
	callCount := 0
	handler := batchHandler(t, resp, &callCount)

	body := feesBatchRequest("ASIN-A")

	// Warm
	req := httptest.NewRequest("POST", "/products/fees/v0/feesEstimate", bytes.NewBufferString(body))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	// HIT - verify bare array format
	req2 := httptest.NewRequest("POST", "/products/fees/v0/feesEstimate", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	respBody, _ := io.ReadAll(w2.Body)
	var parsed []json.RawMessage
	err := json.Unmarshal(respBody, &parsed)
	require.NoError(t, err, "cached fees response should be a bare JSON array")
	assert.Len(t, parsed, 1)
}

// -- Competitive Summary Batch --

func competitiveSummaryBatchRequest(asins ...string) string {
	type req struct {
		ASIN          string   `json:"asin"`
		MarketplaceId string   `json:"marketplaceId"`
		IncludedData  []string `json:"includedData"`
		Method        string   `json:"method"`
		URI           string   `json:"uri"`
	}
	reqs := make([]req, len(asins))
	for i, asin := range asins {
		reqs[i] = req{
			ASIN:          asin,
			MarketplaceId: "ATVPDKIKX0DER",
			IncludedData:  []string{"featuredBuyingOptions", "referencePrices"},
			Method:        "GET",
			URI:           "/products/pricing/2022-05-01/items/competitiveSummary",
		}
	}
	wrapper := map[string]any{"requests": reqs}
	b, _ := json.Marshal(wrapper)
	return string(b)
}

func competitiveSummaryBatchResponse(asins ...string) string {
	type status struct {
		StatusCode   int    `json:"statusCode"`
		ReasonPhrase string `json:"reasonPhrase"`
	}
	type respElem struct {
		Status status `json:"status"`
		Body   any    `json:"body"`
	}
	elems := make([]respElem, len(asins))
	for i, asin := range asins {
		elems[i] = respElem{
			Status: status{StatusCode: 200, ReasonPhrase: "OK"},
			Body:   map[string]any{"asin": asin, "status": "Success"},
		}
	}
	wrapper := map[string]any{"responses": elems}
	b, _ := json.Marshal(wrapper)
	return string(b)
}

func TestBatchCache_CompetitiveSummary_MissAndHit(t *testing.T) {
	resp := competitiveSummaryBatchResponse("B000AAA", "B000BBB")
	callCount := 0
	handler := batchHandler(t, resp, &callCount)

	body := competitiveSummaryBatchRequest("B000AAA", "B000BBB")

	// MISS
	req := httptest.NewRequest("POST", "/batches/products/pricing/2022-05-01/items/competitiveSummary", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, "MISS", w.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)

	// HIT
	req2 := httptest.NewRequest("POST", "/batches/products/pricing/2022-05-01/items/competitiveSummary", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)
}

func TestBatchCache_CompetitiveSummary_CrossBatchHit(t *testing.T) {
	resp := competitiveSummaryBatchResponse("B000AAA", "B000BBB")
	callCount := 0
	handler := batchHandler(t, resp, &callCount)

	// Cache A and B
	body1 := competitiveSummaryBatchRequest("B000AAA", "B000BBB")
	req1 := httptest.NewRequest("POST", "/batches/products/pricing/2022-05-01/items/competitiveSummary", bytes.NewBufferString(body1))
	handler.ServeHTTP(httptest.NewRecorder(), req1)

	// Request just B - should HIT
	body2 := competitiveSummaryBatchRequest("B000BBB")
	req2 := httptest.NewRequest("POST", "/batches/products/pricing/2022-05-01/items/competitiveSummary", bytes.NewBufferString(body2))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)
}

// -- FOEP Batch --

func foepBatchRequest(skus ...string) string {
	type req struct {
		MarketplaceId string `json:"marketplaceId"`
		SKU           string `json:"sku"`
		Method        string `json:"method"`
		URI           string `json:"uri"`
	}
	reqs := make([]req, len(skus))
	for i, sku := range skus {
		reqs[i] = req{
			MarketplaceId: "ATVPDKIKX0DER",
			SKU:           sku,
			Method:        "GET",
			URI:           "/products/pricing/2022-05-01/offer/featuredOfferExpectedPrice",
		}
	}
	wrapper := map[string]any{"requests": reqs}
	b, _ := json.Marshal(wrapper)
	return string(b)
}

func foepBatchResponse(skus ...string) string {
	type status struct {
		StatusCode   int    `json:"statusCode"`
		ReasonPhrase string `json:"reasonPhrase"`
	}
	type respElem struct {
		Status status `json:"status"`
		Body   any    `json:"body"`
	}
	elems := make([]respElem, len(skus))
	for i, sku := range skus {
		elems[i] = respElem{
			Status: status{StatusCode: 200, ReasonPhrase: "OK"},
			Body:   map[string]any{"sku": sku, "status": "Success"},
		}
	}
	wrapper := map[string]any{"responses": elems}
	b, _ := json.Marshal(wrapper)
	return string(b)
}

func TestBatchCache_FOEP_MissAndHit(t *testing.T) {
	resp := foepBatchResponse("SKU-1", "SKU-2")
	callCount := 0
	handler := batchHandler(t, resp, &callCount)

	body := foepBatchRequest("SKU-1", "SKU-2")

	// MISS
	req := httptest.NewRequest("POST", "/batches/products/pricing/2022-05-01/offer/featuredOfferExpectedPrice", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, "MISS", w.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)

	// HIT
	req2 := httptest.NewRequest("POST", "/batches/products/pricing/2022-05-01/offer/featuredOfferExpectedPrice", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)
}

func TestBatchCache_FOEP_OrderIndependent(t *testing.T) {
	resp := foepBatchResponse("SKU-A", "SKU-B")
	callCount := 0
	handler := batchHandler(t, resp, &callCount)

	body1 := foepBatchRequest("SKU-A", "SKU-B")
	req := httptest.NewRequest("POST", "/batches/products/pricing/2022-05-01/offer/featuredOfferExpectedPrice", bytes.NewBufferString(body1))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	// Reversed
	body2 := foepBatchRequest("SKU-B", "SKU-A")
	req2 := httptest.NewRequest("POST", "/batches/products/pricing/2022-05-01/offer/featuredOfferExpectedPrice", bytes.NewBufferString(body2))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)
}

// -- Edge cases --

func TestBatchCache_TTLExpiry(t *testing.T) {
	resp := itemOffersBatchResponse("B000TTL")
	callCount := 0
	handler := batchHandler(t, resp, &callCount)

	body := itemOffersBatchRequest("B000TTL")

	// MISS with short TTL
	req := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body))
	req.Header.Set("X-SP-Proxy-Cache-TTL", "200ms")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, "MISS", w.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)

	// HIT immediately
	req2 := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)

	// Wait for TTL to expire
	time.Sleep(300 * time.Millisecond)

	// MISS after TTL expiry
	req3 := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body))
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req3)
	assert.Equal(t, "MISS", w3.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 2, callCount, "should call upstream after TTL expiry")
}

func TestBatchCache_CustomTTLHeader(t *testing.T) {
	resp := itemOffersBatchResponse("B000CTTL")
	callCount := 0
	handler := batchHandler(t, resp, &callCount)

	body := itemOffersBatchRequest("B000CTTL")

	// Cache with custom TTL
	req := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body))
	req.Header.Set("X-SP-Proxy-Cache-TTL", "10s")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, "MISS", w.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)

	// Should HIT (well within 10s)
	req2 := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount, "custom TTL should keep cache alive")
}

func TestBatchCache_MalformedBody(t *testing.T) {
	resp := itemOffersBatchResponse("B000AAA")
	callCount := 0
	handler := batchHandler(t, resp, &callCount)

	// Send invalid JSON
	req := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString("not valid json {{{"))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
	assert.Equal(t, 1, callCount, "malformed body should pass through to upstream")
}

func TestBatchCache_EmptyRequestsArray(t *testing.T) {
	resp := `{"responses":[]}`
	callCount := 0
	handler := batchHandler(t, resp, &callCount)

	body := `{"requests":[]}`
	req := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
	assert.Equal(t, 1, callCount, "empty requests array should pass through to upstream")
}

func TestBatchCache_MerchantIsolation(t *testing.T) {
	resp := itemOffersBatchResponse("B000ISO")
	callCount := 0

	mc := cache.NewMemoryCache(1 << 20)
	t.Cleanup(func() { mc.Close() })
	tc := cache.NewTierClassifier(nil)
	cfg := &config.CacheConfig{
		Enabled:    true,
		MaxMemory:  1 << 20,
		DefaultTTL: "60s",
	}

	up := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(resp))
	})
	handler := cache.CacheMiddleware(mc, tc, cfg)(up)

	body := itemOffersBatchRequest("B000ISO")

	// Request from merchant-A
	req := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body))
	ctx := merchant.ContextWithMerchant(req.Context(), merchant.ResolvedMerchant{Key: "merchant-A"})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, "MISS", w.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)

	// Same request from merchant-B should MISS (different merchant)
	req2 := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body))
	ctx2 := merchant.ContextWithMerchant(req2.Context(), merchant.ResolvedMerchant{Key: "merchant-B"})
	req2 = req2.WithContext(ctx2)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "MISS", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 2, callCount, "different merchant should have separate cache")

	// Same request from merchant-A should HIT
	req3 := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body))
	ctx3 := merchant.ContextWithMerchant(req3.Context(), merchant.ResolvedMerchant{Key: "merchant-A"})
	req3 = req3.WithContext(ctx3)
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req3)
	assert.Equal(t, "HIT", w3.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 2, callCount, "same merchant should HIT")
}

func TestBatchCache_ErrorElementSkipped(t *testing.T) {
	// Build a response where element A is 200, element B is 400
	type status struct {
		StatusCode   int    `json:"statusCode"`
		ReasonPhrase string `json:"reasonPhrase"`
	}
	type respElem struct {
		Status status `json:"status"`
		Body   any    `json:"body"`
	}
	elems := []respElem{
		{Status: status{StatusCode: 200, ReasonPhrase: "OK"}, Body: map[string]any{"ASIN": "B000OK", "status": "Success"}},
		{Status: status{StatusCode: 400, ReasonPhrase: "Bad Request"}, Body: map[string]any{"errors": []string{"invalid"}}},
	}
	wrapper := map[string]any{"responses": elems}
	respBytes, _ := json.Marshal(wrapper)

	callCount := 0
	handler := batchHandler(t, string(respBytes), &callCount)

	body := itemOffersBatchRequest("B000OK", "B000BAD")

	// First request: MISS
	req := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, "MISS", w.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)

	// Second request with same elements: MISS because B000BAD was not cached
	req2 := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "MISS", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 2, callCount, "should MISS because error element was not cached")
}

func TestBatchCache_Non2xxResponseNotCached(t *testing.T) {
	mc := cache.NewMemoryCache(1 << 20)
	t.Cleanup(func() { mc.Close() })
	tc := cache.NewTierClassifier(nil)
	cfg := &config.CacheConfig{
		Enabled:    true,
		MaxMemory:  1 << 20,
		DefaultTTL: "60s",
	}

	callCount := 0
	errorUpstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write([]byte(`{"errors":["internal server error"]}`))
	})
	handler := addMerchantCtx(cache.CacheMiddleware(mc, tc, cfg)(errorUpstream))

	body := itemOffersBatchRequest("B000ERR")

	// First request
	req := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body))
	handler.ServeHTTP(httptest.NewRecorder(), req)
	assert.Equal(t, 1, callCount)

	// Second request should still MISS (nothing cached from 500)
	req2 := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body))
	handler.ServeHTTP(httptest.NewRecorder(), req2)
	assert.Equal(t, 2, callCount, "500 response should not be cached")
}

func TestBatchCache_HitHeaders(t *testing.T) {
	resp := itemOffersBatchResponse("B000HDR")
	callCount := 0
	handler := batchHandler(t, resp, &callCount)

	body := itemOffersBatchRequest("B000HDR")

	// Warm cache
	req := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	// HIT - verify Content-Type is set
	req2 := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, "application/json", w2.Header().Get("Content-Type"), "batch HIT should set Content-Type")
}

func TestBatchCache_Fees_ErrorElementSkipped(t *testing.T) {
	// Build a fees response where one element is Success, one is ClientError
	type feesElem struct {
		Status                  string `json:"Status"`
		FeesEstimateIdentifier struct {
			IdValue string `json:"IdValue"`
		} `json:"FeesEstimateIdentifier"`
	}
	elems := []feesElem{
		{Status: "Success"},
		{Status: "ClientError"},
	}
	elems[0].FeesEstimateIdentifier.IdValue = "ASIN-OK"
	elems[1].FeesEstimateIdentifier.IdValue = "ASIN-BAD"
	respBytes, _ := json.Marshal(elems)

	callCount := 0
	handler := batchHandler(t, string(respBytes), &callCount)

	body := feesBatchRequest("ASIN-OK", "ASIN-BAD")

	// First request: MISS
	req := httptest.NewRequest("POST", "/products/fees/v0/feesEstimate", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, "MISS", w.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)

	// Second request: MISS because ASIN-BAD was not cached
	req2 := httptest.NewRequest("POST", "/products/fees/v0/feesEstimate", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "MISS", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 2, callCount, "should MISS because error element was not cached")
}

func TestBatchCache_SingleElementBatch(t *testing.T) {
	resp := itemOffersBatchResponse("B000SINGLE")
	callCount := 0
	handler := batchHandler(t, resp, &callCount)

	body := itemOffersBatchRequest("B000SINGLE")

	// MISS
	req := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, "MISS", w.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)

	// HIT
	req2 := httptest.NewRequest("POST", "/batches/products/pricing/v0/itemOffers", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount, "single element batch should cache correctly")
}
