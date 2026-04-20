package cache_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spiohq/smart-proxy/internal/cache"
	"github.com/spiohq/smart-proxy/internal/config"
	"github.com/spiohq/smart-proxy/internal/merchant"
	"github.com/stretchr/testify/assert"
)

func postCacheHandler(t *testing.T, respBody string, callCount *int) http.Handler {
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
		// Verify body is still readable by upstream
		body, _ := io.ReadAll(r.Body)
		_ = body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(respBody))
	})
	return addMerchantCtx(cache.CacheMiddleware(mc, tc, cfg)(up))
}

// -- Single-item fees --

func TestPostCache_FeesItemASIN_MissAndHit(t *testing.T) {
	callCount := 0
	handler := postCacheHandler(t, `{"fees":"result"}`, &callCount)

	body := `{"FeesEstimateRequest":{"MarketplaceId":"ATVPDKIKX0DER","PriceToEstimateFees":{"ListingPrice":{"CurrencyCode":"USD","Amount":25.99}},"Identifier":"req-1","IsAmazonFulfilled":true}}`

	// MISS
	req := httptest.NewRequest("POST", "/products/fees/v0/items/B07XJ8C8F5/feesEstimate", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, "MISS", w.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)

	// HIT
	req2 := httptest.NewRequest("POST", "/products/fees/v0/items/B07XJ8C8F5/feesEstimate", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)
}

func TestPostCache_FeesItemSKU_MissAndHit(t *testing.T) {
	callCount := 0
	handler := postCacheHandler(t, `{"fees":"sku-result"}`, &callCount)

	body := `{"FeesEstimateRequest":{"MarketplaceId":"ATVPDKIKX0DER","PriceToEstimateFees":{"ListingPrice":{"CurrencyCode":"USD","Amount":15.00}},"Identifier":"req-2"}}`

	// MISS
	req := httptest.NewRequest("POST", "/products/fees/v0/listings/MY-SKU-123/feesEstimate", bytes.NewBufferString(body))
	handler.ServeHTTP(httptest.NewRecorder(), req)
	assert.Equal(t, 1, callCount)

	// HIT
	req2 := httptest.NewRequest("POST", "/products/fees/v0/listings/MY-SKU-123/feesEstimate", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)
}

func TestPostCache_Fees_IdentifierIgnored(t *testing.T) {
	callCount := 0
	handler := postCacheHandler(t, `{"fees":"result"}`, &callCount)

	body1 := `{"FeesEstimateRequest":{"MarketplaceId":"ATVPDKIKX0DER","PriceToEstimateFees":{"ListingPrice":{"CurrencyCode":"USD","Amount":25.99}},"Identifier":"request-AAA"}}`
	body2 := `{"FeesEstimateRequest":{"MarketplaceId":"ATVPDKIKX0DER","PriceToEstimateFees":{"ListingPrice":{"CurrencyCode":"USD","Amount":25.99}},"Identifier":"request-ZZZ"}}`

	// MISS with Identifier AAA
	req := httptest.NewRequest("POST", "/products/fees/v0/items/B000TEST/feesEstimate", bytes.NewBufferString(body1))
	handler.ServeHTTP(httptest.NewRecorder(), req)
	assert.Equal(t, 1, callCount)

	// HIT with Identifier ZZZ (only Identifier differs)
	req2 := httptest.NewRequest("POST", "/products/fees/v0/items/B000TEST/feesEstimate", bytes.NewBufferString(body2))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount, "different Identifier should still HIT")
}

func TestPostCache_Fees_DifferentASIN_Miss(t *testing.T) {
	callCount := 0
	handler := postCacheHandler(t, `{"fees":"result"}`, &callCount)

	body := `{"FeesEstimateRequest":{"MarketplaceId":"ATVPDKIKX0DER","PriceToEstimateFees":{"ListingPrice":{"CurrencyCode":"USD","Amount":25.99}},"Identifier":"req-1"}}`

	// Cache for ASIN-A
	req := httptest.NewRequest("POST", "/products/fees/v0/items/ASIN-A/feesEstimate", bytes.NewBufferString(body))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	// Different ASIN -> MISS (path differs)
	req2 := httptest.NewRequest("POST", "/products/fees/v0/items/ASIN-B/feesEstimate", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "MISS", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 2, callCount)
}

func TestPostCache_Fees_DifferentPrice_Miss(t *testing.T) {
	callCount := 0
	handler := postCacheHandler(t, `{"fees":"result"}`, &callCount)

	body1 := `{"FeesEstimateRequest":{"MarketplaceId":"ATVPDKIKX0DER","PriceToEstimateFees":{"ListingPrice":{"CurrencyCode":"USD","Amount":10.00}},"Identifier":"req-1"}}`
	body2 := `{"FeesEstimateRequest":{"MarketplaceId":"ATVPDKIKX0DER","PriceToEstimateFees":{"ListingPrice":{"CurrencyCode":"USD","Amount":99.99}},"Identifier":"req-1"}}`

	req := httptest.NewRequest("POST", "/products/fees/v0/items/B000TEST/feesEstimate", bytes.NewBufferString(body1))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	req2 := httptest.NewRequest("POST", "/products/fees/v0/items/B000TEST/feesEstimate", bytes.NewBufferString(body2))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "MISS", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 2, callCount, "different price should MISS")
}

// -- Shipping v2 rates --

func TestPostCache_ShippingV2Rates_MissAndHit(t *testing.T) {
	callCount := 0
	handler := postCacheHandler(t, `{"rates":[{"serviceType":"Standard","charge":{"amount":5.99}}]}`, &callCount)

	body := `{"shipFrom":{"postalCode":"98109","countryCode":"US"},"shipTo":{"postalCode":"10001","countryCode":"US"},"packages":[{"dimensions":{"length":10,"width":8,"height":6,"unit":"INCH"},"weight":{"value":2,"unit":"POUND"}}],"channelDetails":{"channelType":"EXTERNAL"}}`

	// MISS
	req := httptest.NewRequest("POST", "/shipping/v2/shipments/rates", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, "MISS", w.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)

	// HIT
	req2 := httptest.NewRequest("POST", "/shipping/v2/shipments/rates", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)
}

func TestPostCache_ShippingV2Rates_ClientRefIgnored(t *testing.T) {
	callCount := 0
	handler := postCacheHandler(t, `{"rates":[]}`, &callCount)

	body1 := `{"shipFrom":{"postalCode":"98109"},"packages":[],"channelDetails":{"channelType":"EXTERNAL"},"clientReferenceDetails":[{"clientReferenceType":"IntegratorShipperId","clientReferenceId":"ref-AAA"}]}`
	body2 := `{"shipFrom":{"postalCode":"98109"},"packages":[],"channelDetails":{"channelType":"EXTERNAL"},"clientReferenceDetails":[{"clientReferenceType":"IntegratorShipperId","clientReferenceId":"ref-ZZZ"}]}`

	req := httptest.NewRequest("POST", "/shipping/v2/shipments/rates", bytes.NewBufferString(body1))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	req2 := httptest.NewRequest("POST", "/shipping/v2/shipments/rates", bytes.NewBufferString(body2))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount, "different clientReferenceDetails should still HIT")
}

// -- Shipping v1 rates --

func TestPostCache_ShippingV1Rates_MissAndHit(t *testing.T) {
	callCount := 0
	handler := postCacheHandler(t, `{"rates":[]}`, &callCount)

	body := `{"shipTo":{"postalCode":"10001","countryCode":"US"},"shipFrom":{"postalCode":"98109","countryCode":"US"},"serviceTypes":["Standard"],"containerSpecifications":[{"dimensions":{"length":10,"width":10,"height":10,"unit":"CM"},"weight":{"value":1,"unit":"kg"}}]}`

	req := httptest.NewRequest("POST", "/shipping/v1/rates", bytes.NewBufferString(body))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	req2 := httptest.NewRequest("POST", "/shipping/v1/rates", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)
}

// -- Eligible shipping services --

func TestPostCache_EligibleShippingServices_MissAndHit(t *testing.T) {
	callCount := 0
	handler := postCacheHandler(t, `{"ShippingServiceList":[]}`, &callCount)

	body := `{"ShipmentRequestDetails":{"AmazonOrderId":"903-5563053-5647845","ItemList":[{"OrderItemId":"123","Quantity":1}],"ShipFromAddress":{"Name":"Test","AddressLine1":"123 St","City":"Detroit","StateOrProvinceCode":"MI","PostalCode":"48123","CountryCode":"US"},"PackageDimensions":{"Length":10,"Width":10,"Height":10,"Unit":"inches"},"Weight":{"Value":10,"Unit":"oz"},"ShippingServiceOptions":{"DeliveryExperience":"NoTracking","CarrierWillPickUp":false}}}`

	req := httptest.NewRequest("POST", "/mfn/v0/eligibleShippingServices", bytes.NewBufferString(body))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	req2 := httptest.NewRequest("POST", "/mfn/v0/eligibleShippingServices", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)
}

// -- FBA fulfillment preview --

func TestPostCache_FulfillmentPreview_MissAndHit(t *testing.T) {
	callCount := 0
	handler := postCacheHandler(t, `{"fulfillmentPreviews":[]}`, &callCount)

	body := `{"address":{"name":"Test","addressLine1":"123 St","city":"Seattle","stateOrRegion":"WA","postalCode":"98109","countryCode":"US"},"items":[{"sellerSku":"SKU123","quantity":1,"sellerFulfillmentOrderItemId":"item1"}],"marketplaceId":"ATVPDKIKX0DER","shippingSpeedCategories":["Standard"]}`

	req := httptest.NewRequest("POST", "/fba/outbound/2020-07-01/fulfillmentOrders/preview", bytes.NewBufferString(body))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	req2 := httptest.NewRequest("POST", "/fba/outbound/2020-07-01/fulfillmentOrders/preview", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)
}

// -- EasyShip time slots --

func TestPostCache_EasyShipTimeSlot_MissAndHit(t *testing.T) {
	callCount := 0
	handler := postCacheHandler(t, `{"amazonOrderId":"123","timeSlots":[]}`, &callCount)

	body := `{"marketplaceId":"A21TJRUUN4KGV","amazonOrderId":"931-2308757-7991048","packageDimensions":{"length":15,"width":10,"height":12,"unit":"cm"},"packageWeight":{"value":50,"unit":"g"}}`

	req := httptest.NewRequest("POST", "/easyShip/2022-03-23/timeSlot", bytes.NewBufferString(body))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	req2 := httptest.NewRequest("POST", "/easyShip/2022-03-23/timeSlot", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)
}

// -- MFN additional seller inputs --

func TestPostCache_AdditionalSellerInputs_MissAndHit(t *testing.T) {
	callCount := 0
	handler := postCacheHandler(t, `{"ShipmentLevelFields":[]}`, &callCount)

	body := `{"ShippingServiceId":"UPS_PTP_2ND_DAY_AIR","ShipFromAddress":{"Name":"Test","AddressLine1":"123 St","City":"Detroit","StateOrProvinceCode":"MI","PostalCode":"48123","CountryCode":"US","Phone":"555-1234"},"OrderId":"903-5563053-5647845"}`

	req := httptest.NewRequest("POST", "/mfn/v0/additionalSellerInputs", bytes.NewBufferString(body))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	req2 := httptest.NewRequest("POST", "/mfn/v0/additionalSellerInputs", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)
}

// -- Replenishment metrics --

func TestPostCache_ReplenishmentSellerMetrics_MissAndHit(t *testing.T) {
	callCount := 0
	handler := postCacheHandler(t, `{"metrics":[]}`, &callCount)

	body := `{"timeInterval":{"startDate":"2024-01-01T00:00:00Z","endDate":"2024-02-01T00:00:00Z"},"timePeriodType":"PERFORMANCE","programTypes":["SUBSCRIBE_AND_SAVE"],"marketplaceId":"ATVPDKIKX0DER"}`

	req := httptest.NewRequest("POST", "/replenishment/2022-11-07/sellingPartners/metrics/search", bytes.NewBufferString(body))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	req2 := httptest.NewRequest("POST", "/replenishment/2022-11-07/sellingPartners/metrics/search", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)
}

func TestPostCache_ReplenishmentOfferMetrics_MissAndHit(t *testing.T) {
	callCount := 0
	handler := postCacheHandler(t, `{"offers":[]}`, &callCount)

	body := `{"pagination":{"limit":100,"offset":0},"filters":{"timeInterval":{"startDate":"2024-01-01T00:00:00Z","endDate":"2024-02-01T00:00:00Z"},"timePeriodType":"PERFORMANCE","programTypes":["SUBSCRIBE_AND_SAVE"],"marketplaceId":"ATVPDKIKX0DER"}}`

	req := httptest.NewRequest("POST", "/replenishment/2022-11-07/offers/metrics/search", bytes.NewBufferString(body))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	req2 := httptest.NewRequest("POST", "/replenishment/2022-11-07/offers/metrics/search", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)
}

func TestPostCache_ReplenishmentOffersSearch_MissAndHit(t *testing.T) {
	callCount := 0
	handler := postCacheHandler(t, `{"offers":[]}`, &callCount)

	body := `{"pagination":{"limit":100,"offset":0},"filters":{"marketplaceId":"ATVPDKIKX0DER","programTypes":["SUBSCRIBE_AND_SAVE"]}}`

	req := httptest.NewRequest("POST", "/replenishment/2022-11-07/offers/search", bytes.NewBufferString(body))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	req2 := httptest.NewRequest("POST", "/replenishment/2022-11-07/offers/search", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)
}

// -- Bypass for POST-cacheable endpoints --

func TestPostCache_Bypass(t *testing.T) {
	callCount := 0
	handler := postCacheHandler(t, `{"result":"data"}`, &callCount)

	body := `{"FeesEstimateRequest":{"MarketplaceId":"ATVPDKIKX0DER","PriceToEstimateFees":{"ListingPrice":{"CurrencyCode":"USD","Amount":25.99}},"Identifier":"req-1"}}`

	// Warm cache
	req := httptest.NewRequest("POST", "/products/fees/v0/items/B000TEST/feesEstimate", bytes.NewBufferString(body))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	// Bypass
	req2 := httptest.NewRequest("POST", "/products/fees/v0/items/B000TEST/feesEstimate", bytes.NewBufferString(body))
	req2.Header.Set("X-SP-Proxy-No-Cache", "true")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "BYPASS", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 2, callCount)
}

// -- Non-2xx not cached --

func TestPostCache_Non2xxNotCached(t *testing.T) {
	mc := cache.NewMemoryCache(1 << 20)
	defer mc.Close()
	tc := cache.NewTierClassifier(nil)
	cfg := &config.CacheConfig{Enabled: true, MaxMemory: 1 << 20, DefaultTTL: "60s"}

	callCount := 0
	errorUpstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(400)
		w.Write([]byte(`{"errors":["bad request"]}`))
	})
	handler := addMerchantCtx(cache.CacheMiddleware(mc, tc, cfg)(errorUpstream))

	body := `{"FeesEstimateRequest":{"MarketplaceId":"X","PriceToEstimateFees":{"ListingPrice":{"CurrencyCode":"USD","Amount":1}},"Identifier":"r1"}}`

	req := httptest.NewRequest("POST", "/products/fees/v0/items/B000/feesEstimate", bytes.NewBufferString(body))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	req2 := httptest.NewRequest("POST", "/products/fees/v0/items/B000/feesEstimate", bytes.NewBufferString(body))
	handler.ServeHTTP(httptest.NewRecorder(), req2)
	assert.Equal(t, 2, callCount, "error responses should not be cached")
}

// -- Body is still readable by upstream --

func TestPostCache_BodyPassedThrough(t *testing.T) {
	var receivedBody string
	mc := cache.NewMemoryCache(1 << 20)
	defer mc.Close()
	tc := cache.NewTierClassifier(nil)
	cfg := &config.CacheConfig{Enabled: true, MaxMemory: 1 << 20, DefaultTTL: "60s"}

	up := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		receivedBody = string(b)
		w.WriteHeader(200)
		w.Write([]byte(`{"ok":true}`))
	})
	handler := addMerchantCtx(cache.CacheMiddleware(mc, tc, cfg)(up))

	body := `{"shipTo":{"postalCode":"10001"},"shipFrom":{"postalCode":"98109"},"serviceTypes":["Standard"],"containerSpecifications":[]}`
	req := httptest.NewRequest("POST", "/shipping/v1/rates", bytes.NewBufferString(body))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, body, receivedBody, "upstream should receive the original body")
}

// -- Edge cases --

func TestPostCache_TTLExpiry(t *testing.T) {
	callCount := 0
	handler := postCacheHandler(t, `{"fees":"result"}`, &callCount)

	body := `{"FeesEstimateRequest":{"MarketplaceId":"ATVPDKIKX0DER","PriceToEstimateFees":{"ListingPrice":{"CurrencyCode":"USD","Amount":25.99}},"Identifier":"ttl-test"}}`

	// MISS with short TTL
	req := httptest.NewRequest("POST", "/products/fees/v0/items/B000TTL/feesEstimate", bytes.NewBufferString(body))
	req.Header.Set("X-SP-Proxy-Cache-TTL", "200ms")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, "MISS", w.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)

	// HIT immediately
	req2 := httptest.NewRequest("POST", "/products/fees/v0/items/B000TTL/feesEstimate", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)

	// Wait for TTL to expire
	time.Sleep(300 * time.Millisecond)

	// MISS after TTL expiry
	req3 := httptest.NewRequest("POST", "/products/fees/v0/items/B000TTL/feesEstimate", bytes.NewBufferString(body))
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req3)
	assert.Equal(t, "MISS", w3.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 2, callCount, "should call upstream after TTL expiry")
}

func TestPostCache_CustomTTLHeader(t *testing.T) {
	callCount := 0
	handler := postCacheHandler(t, `{"fees":"result"}`, &callCount)

	body := `{"FeesEstimateRequest":{"MarketplaceId":"ATVPDKIKX0DER","PriceToEstimateFees":{"ListingPrice":{"CurrencyCode":"USD","Amount":25.99}},"Identifier":"cttl"}}`

	// Cache with custom TTL
	req := httptest.NewRequest("POST", "/products/fees/v0/items/B000CTTL/feesEstimate", bytes.NewBufferString(body))
	req.Header.Set("X-SP-Proxy-Cache-TTL", "10s")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, "MISS", w.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)

	// Should HIT (well within 10s)
	req2 := httptest.NewRequest("POST", "/products/fees/v0/items/B000CTTL/feesEstimate", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount, "custom TTL should keep cache alive")
}

func TestPostCache_MalformedBody(t *testing.T) {
	callCount := 0
	handler := postCacheHandler(t, `{"result":"ok"}`, &callCount)

	// Send non-JSON body
	req := httptest.NewRequest("POST", "/products/fees/v0/items/B000MAL/feesEstimate", bytes.NewBufferString("not json at all"))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
	assert.Equal(t, 1, callCount, "malformed body should pass through to upstream")
}

func TestPostCache_EmptyBody(t *testing.T) {
	callCount := 0
	handler := postCacheHandler(t, `{"result":"ok"}`, &callCount)

	// Send empty body
	req := httptest.NewRequest("POST", "/products/fees/v0/items/B000EMPTY/feesEstimate", bytes.NewBufferString(""))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)
	assert.Equal(t, 1, callCount, "empty body should pass through to upstream")
}

func TestPostCache_MerchantIsolation(t *testing.T) {
	mc := cache.NewMemoryCache(1 << 20)
	t.Cleanup(func() { mc.Close() })
	tc := cache.NewTierClassifier(nil)
	cfg := &config.CacheConfig{
		Enabled:    true,
		MaxMemory:  1 << 20,
		DefaultTTL: "60s",
	}

	callCount := 0
	up := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := io.ReadAll(r.Body)
		_ = body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"fees":"result"}`))
	})
	handler := cache.CacheMiddleware(mc, tc, cfg)(up)

	body := `{"FeesEstimateRequest":{"MarketplaceId":"ATVPDKIKX0DER","PriceToEstimateFees":{"ListingPrice":{"CurrencyCode":"USD","Amount":25.99}},"Identifier":"iso"}}`

	// Request from merchant-A
	req := httptest.NewRequest("POST", "/products/fees/v0/items/B000ISO/feesEstimate", bytes.NewBufferString(body))
	ctx := merchant.ContextWithMerchant(req.Context(), merchant.ResolvedMerchant{Key: "merchant-A"})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, "MISS", w.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)

	// Same request from merchant-B should MISS
	req2 := httptest.NewRequest("POST", "/products/fees/v0/items/B000ISO/feesEstimate", bytes.NewBufferString(body))
	ctx2 := merchant.ContextWithMerchant(req2.Context(), merchant.ResolvedMerchant{Key: "merchant-B"})
	req2 = req2.WithContext(ctx2)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "MISS", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 2, callCount, "different merchant should have separate cache")

	// Same request from merchant-A should HIT
	req3 := httptest.NewRequest("POST", "/products/fees/v0/items/B000ISO/feesEstimate", bytes.NewBufferString(body))
	ctx3 := merchant.ContextWithMerchant(req3.Context(), merchant.ResolvedMerchant{Key: "merchant-A"})
	req3 = req3.WithContext(ctx3)
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req3)
	assert.Equal(t, "HIT", w3.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 2, callCount, "same merchant should HIT")
}

func TestPostCache_NestedIdentifierStripped(t *testing.T) {
	callCount := 0
	handler := postCacheHandler(t, `{"fees":"result"}`, &callCount)

	// Body with nested Identifier inside FeesEstimateRequest
	body1 := `{"FeesEstimateRequest":{"MarketplaceId":"ATVPDKIKX0DER","PriceToEstimateFees":{"ListingPrice":{"CurrencyCode":"USD","Amount":25.99}},"Identifier":"nested-AAA","IsAmazonFulfilled":true}}`
	body2 := `{"FeesEstimateRequest":{"MarketplaceId":"ATVPDKIKX0DER","PriceToEstimateFees":{"ListingPrice":{"CurrencyCode":"USD","Amount":25.99}},"Identifier":"nested-ZZZ","IsAmazonFulfilled":true}}`

	// MISS with first Identifier
	req := httptest.NewRequest("POST", "/products/fees/v0/items/B000NEST/feesEstimate", bytes.NewBufferString(body1))
	handler.ServeHTTP(httptest.NewRecorder(), req)
	assert.Equal(t, 1, callCount)

	// HIT with different nested Identifier (should be stripped)
	req2 := httptest.NewRequest("POST", "/products/fees/v0/items/B000NEST/feesEstimate", bytes.NewBufferString(body2))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount, "different nested Identifier should still HIT")
}

func TestPostCache_LargeBody(t *testing.T) {
	callCount := 0
	handler := postCacheHandler(t, `{"rates":[{"serviceType":"Standard","charge":{"amount":5.99}}]}`, &callCount)

	// Build a large body with many packages
	type dim struct {
		Length int    `json:"length"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
		Unit   string `json:"unit"`
	}
	type weight struct {
		Value int    `json:"value"`
		Unit  string `json:"unit"`
	}
	type pkg struct {
		Dimensions dim    `json:"dimensions"`
		Weight     weight `json:"weight"`
	}
	packages := make([]pkg, 50)
	for i := range packages {
		packages[i] = pkg{
			Dimensions: dim{Length: 10 + i, Width: 8, Height: 6, Unit: "INCH"},
			Weight:     weight{Value: 2 + i, Unit: "POUND"},
		}
	}
	largeBody := map[string]any{
		"shipFrom":       map[string]string{"postalCode": "98109", "countryCode": "US"},
		"shipTo":         map[string]string{"postalCode": "10001", "countryCode": "US"},
		"packages":       packages,
		"channelDetails": map[string]string{"channelType": "EXTERNAL"},
	}
	bodyBytes, _ := json.Marshal(largeBody)
	body := string(bodyBytes)

	// MISS
	req := httptest.NewRequest("POST", "/shipping/v2/shipments/rates", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, "MISS", w.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)

	// HIT
	req2 := httptest.NewRequest("POST", "/shipping/v2/shipments/rates", bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "HIT", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount, "large body should still cache correctly")
}

func TestPostCache_SameBodyDifferentPath(t *testing.T) {
	callCount := 0
	handler := postCacheHandler(t, `{"fees":"result"}`, &callCount)

	body := `{"FeesEstimateRequest":{"MarketplaceId":"ATVPDKIKX0DER","PriceToEstimateFees":{"ListingPrice":{"CurrencyCode":"USD","Amount":25.99}},"Identifier":"path-test"}}`

	// Cache for ASIN-A path
	req := httptest.NewRequest("POST", "/products/fees/v0/items/ASIN-A/feesEstimate", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, "MISS", w.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 1, callCount)

	// Same body to different path (ASIN-B) should MISS
	req2 := httptest.NewRequest("POST", fmt.Sprintf("/products/fees/v0/items/ASIN-B/feesEstimate"), bytes.NewBufferString(body))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, "MISS", w2.Header().Get("X-SP-Proxy-Cache"))
	assert.Equal(t, 2, callCount, "same body to different path should produce separate cache entries")
}
