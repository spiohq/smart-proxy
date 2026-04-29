package cache

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/spiohq/smart-proxy/internal/config"
	"github.com/spiohq/smart-proxy/internal/merchant"
)

// batchEndpointConfig describes how to extract per-element cache keys and
// reassemble responses for a batch endpoint.
type batchEndpointConfig struct {
	format     batchBodyFormat
	extractKey func(element json.RawMessage) (string, error) // returns a cache-key suffix per element
	wrapReq    string                                        // JSON field that wraps the request array ("requests" or "")
	wrapResp   string                                        // JSON field that wraps the response array ("responses" or "")
}

type batchBodyFormat int

const (
	batchFormatWrapped batchBodyFormat = iota // {"requests": [...]}  /  {"responses": [...]}
	batchFormatBare                           // bare JSON array [...]
)

// batchEndpointRegistry maps path prefixes to their config.
var batchEndpointRegistry = map[string]*batchEndpointConfig{
	"/batches/products/pricing/v0/itemOffers": {
		format:     batchFormatWrapped,
		extractKey: extractItemOffersKey,
		wrapReq:    "requests",
		wrapResp:   "responses",
	},
	"/batches/products/pricing/v0/listingOffers": {
		format:     batchFormatWrapped,
		extractKey: extractListingOffersKey,
		wrapReq:    "requests",
		wrapResp:   "responses",
	},
	"/products/fees/v0/feesEstimate": {
		format:     batchFormatBare,
		extractKey: extractFeesKey,
	},
	"/batches/products/pricing/2022-05-01/items/competitiveSummary": {
		format:     batchFormatWrapped,
		extractKey: extractCompetitiveSummaryKey,
		wrapReq:    "requests",
		wrapResp:   "responses",
	},
	"/batches/products/pricing/2022-05-01/offer/featuredOfferExpectedPrice": {
		format:     batchFormatWrapped,
		extractKey: extractFOEPKey,
		wrapReq:    "requests",
		wrapResp:   "responses",
	},
}

// lookupBatchConfig returns the batch config for the given path, or nil.
func lookupBatchConfig(path string) *batchEndpointConfig {
	for prefix, cfg := range batchEndpointRegistry {
		if strings.HasPrefix(path, prefix) {
			return cfg
		}
	}
	return nil
}

// -- Key extractors per endpoint type --

// extractItemOffersKey extracts "ASIN:MarketplaceId:ItemCondition:CustomerType"
// from a getItemOffersBatch request element.
func extractItemOffersKey(elem json.RawMessage) (string, error) {
	var req struct {
		URI            string `json:"uri"`
		MarketplaceId  string `json:"MarketplaceId"`
		ItemCondition  string `json:"ItemCondition"`
		CustomerType   string `json:"CustomerType"`
	}
	if err := json.Unmarshal(elem, &req); err != nil {
		return "", err
	}
	// Extract ASIN from uri: /products/pricing/v0/items/{ASIN}/offers
	asin := extractPathSegment(req.URI, "/products/pricing/v0/items/", "/offers")
	if asin == "" {
		return "", fmt.Errorf("cannot extract ASIN from uri: %s", req.URI)
	}
	ct := req.CustomerType
	if ct == "" {
		ct = "Consumer"
	}
	return fmt.Sprintf("itemOffers:%s:%s:%s:%s", asin, req.MarketplaceId, req.ItemCondition, ct), nil
}

// extractListingOffersKey extracts "SKU:MarketplaceId:ItemCondition:CustomerType"
// from a getListingOffersBatch request element.
func extractListingOffersKey(elem json.RawMessage) (string, error) {
	var req struct {
		URI            string `json:"uri"`
		MarketplaceId  string `json:"MarketplaceId"`
		ItemCondition  string `json:"ItemCondition"`
		CustomerType   string `json:"CustomerType"`
	}
	if err := json.Unmarshal(elem, &req); err != nil {
		return "", err
	}
	// Extract SKU from uri: /products/pricing/v0/listings/{SKU}/offers
	sku := extractPathSegment(req.URI, "/products/pricing/v0/listings/", "/offers")
	if sku == "" {
		return "", fmt.Errorf("cannot extract SKU from uri: %s", req.URI)
	}
	ct := req.CustomerType
	if ct == "" {
		ct = "Consumer"
	}
	return fmt.Sprintf("listingOffers:%s:%s:%s:%s", sku, req.MarketplaceId, req.ItemCondition, ct), nil
}

// extractFeesKey extracts "IdType:IdValue:MarketplaceId:IsAmazonFulfilled:ListingPrice"
// from a getMyFeesEstimates request element.
func extractFeesKey(elem json.RawMessage) (string, error) {
	var req struct {
		IdType  string `json:"IdType"`
		IdValue string `json:"IdValue"`
		FER     struct {
			MarketplaceId      string `json:"MarketplaceId"`
			IsAmazonFulfilled  *bool  `json:"IsAmazonFulfilled"`
			PriceToEstimate    struct {
				ListingPrice struct {
					CurrencyCode string  `json:"CurrencyCode"`
					Amount       float64 `json:"Amount"`
				} `json:"ListingPrice"`
				Shipping *struct {
					CurrencyCode string  `json:"CurrencyCode"`
					Amount       float64 `json:"Amount"`
				} `json:"Shipping"`
			} `json:"PriceToEstimateFees"`
			OptionalFulfillmentProgram string `json:"OptionalFulfillmentProgram"`
		} `json:"FeesEstimateRequest"`
	}
	if err := json.Unmarshal(elem, &req); err != nil {
		return "", err
	}
	fulfilled := "nil"
	if req.FER.IsAmazonFulfilled != nil {
		if *req.FER.IsAmazonFulfilled {
			fulfilled = "true"
		} else {
			fulfilled = "false"
		}
	}
	shippingAmt := 0.0
	if req.FER.PriceToEstimate.Shipping != nil {
		shippingAmt = req.FER.PriceToEstimate.Shipping.Amount
	}
	return fmt.Sprintf("fees:%s:%s:%s:%s:%.2f:%s:%.2f:%s",
		req.IdType, req.IdValue,
		req.FER.MarketplaceId, fulfilled,
		req.FER.PriceToEstimate.ListingPrice.Amount,
		req.FER.PriceToEstimate.ListingPrice.CurrencyCode,
		shippingAmt,
		req.FER.OptionalFulfillmentProgram,
	), nil
}

// extractCompetitiveSummaryKey extracts "ASIN:MarketplaceId:includedData"
// from a getCompetitiveSummary batch request element.
func extractCompetitiveSummaryKey(elem json.RawMessage) (string, error) {
	var req struct {
		ASIN          string   `json:"asin"`
		MarketplaceId string   `json:"marketplaceId"`
		IncludedData  []string `json:"includedData"`
	}
	if err := json.Unmarshal(elem, &req); err != nil {
		return "", err
	}
	if req.ASIN == "" || req.MarketplaceId == "" {
		return "", fmt.Errorf("missing asin or marketplaceId")
	}
	// Sort includedData for deterministic keys
	sorted := make([]string, len(req.IncludedData))
	copy(sorted, req.IncludedData)
	sort.Strings(sorted)
	return fmt.Sprintf("competitiveSummary:%s:%s:%s",
		req.ASIN, req.MarketplaceId, strings.Join(sorted, ",")), nil
}

// extractFOEPKey extracts "SKU:MarketplaceId" from a
// getFeaturedOfferExpectedPriceBatch request element.
func extractFOEPKey(elem json.RawMessage) (string, error) {
	var req struct {
		MarketplaceId string `json:"marketplaceId"`
		SKU           string `json:"sku"`
	}
	if err := json.Unmarshal(elem, &req); err != nil {
		return "", err
	}
	if req.SKU == "" || req.MarketplaceId == "" {
		return "", fmt.Errorf("missing sku or marketplaceId")
	}
	return fmt.Sprintf("foep:%s:%s", req.SKU, req.MarketplaceId), nil
}

// extractPathSegment extracts the value between prefix and suffix in a URI path.
// e.g. extractPathSegment("/products/pricing/v0/items/B000XYZ/offers",
//
//	"/products/pricing/v0/items/", "/offers") => "B000XYZ"
func extractPathSegment(uri, prefix, suffix string) string {
	if !strings.HasPrefix(uri, prefix) {
		return ""
	}
	rest := uri[len(prefix):]
	if suffix != "" {
		idx := strings.Index(rest, suffix)
		if idx < 0 {
			return ""
		}
		rest = rest[:idx]
	}
	return rest
}

// -- Per-element cache key --

func batchElementCacheKey(merchantKey, path, elementSuffix string) string {
	return merchantKey + ":BATCH:" + path + ":" + elementSuffix
}

// -- Batch cache middleware logic --

// handleBatchCache processes a batch POST request with per-element caching.
// If ALL elements are cached, it returns the assembled response (full HIT).
// Otherwise it forwards the full request upstream and caches each element individually.
func handleBatchCache(
	w http.ResponseWriter, r *http.Request,
	next http.Handler,
	c Cache, tier TierConfig, cfg *config.CacheConfig,
	bcfg *batchEndpointConfig,
) {
	m := merchant.MerchantFromContext(r.Context())

	// Read body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Warn("batch cache: failed to read body", "path", r.URL.Path, "error", err)
		next.ServeHTTP(w, r)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Parse elements from request body
	elements, err := parseBatchElements(bodyBytes, bcfg)
	if err != nil || len(elements) == 0 {
		slog.Warn("batch cache: failed to parse elements", "path", r.URL.Path, "error", err)
		next.ServeHTTP(w, r)
		return
	}

	// Extract per-element cache keys
	entries := make([]elementEntry, len(elements))
	allCached := true
	for i, elem := range elements {
		suffix, err := bcfg.extractKey(elem)
		if err != nil {
			slog.Warn("batch cache: failed to extract key", "index", i, "error", err)
			allCached = false
			entries[i] = elementEntry{raw: elem}
			continue
		}
		cacheKey := batchElementCacheKey(m.Key, r.URL.Path, suffix)
		entries[i] = elementEntry{raw: elem, key: cacheKey}

		// Try cache lookup
		cached, err := c.Get(r.Context(), cacheKey)
		if err == nil && cached != nil {
			entries[i].cached = cached
		} else {
			allCached = false
		}
	}

	// Full HIT: assemble response from cached elements
	if allCached {
		assembled, err := assembleBatchResponse(entries, bcfg)
		if err != nil {
			slog.Warn("batch cache: failed to assemble cached response", "error", err)
			// Fall through to upstream
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-SP-Proxy-Cache", "HIT")
			w.WriteHeader(http.StatusOK)
			w.Write(assembled)
			return
		}
	}

	// MISS: forward full request upstream, then cache each element
	rec := newResponseRecorder()
	next.ServeHTTP(rec, r)

	// Copy upstream response to client
	for k, vals := range rec.headers {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("X-SP-Proxy-Cache", "MISS")
	w.WriteHeader(rec.statusCode)
	w.Write(rec.body.Bytes())

	// Only cache individual elements from 2xx responses
	if rec.statusCode >= 200 && rec.statusCode < 300 {
		cacheResponseElements(r, c, rec.body.Bytes(), entries, bcfg, tier, cfg)
	}
}

// parseBatchElements extracts the raw JSON elements from a batch request body.
func parseBatchElements(body []byte, bcfg *batchEndpointConfig) ([]json.RawMessage, error) {
	if bcfg.format == batchFormatWrapped {
		var wrapper map[string]json.RawMessage
		if err := json.Unmarshal(body, &wrapper); err != nil {
			return nil, err
		}
		field := bcfg.wrapReq
		arr, ok := wrapper[field]
		if !ok {
			return nil, fmt.Errorf("missing field %q in batch request", field)
		}
		var elements []json.RawMessage
		if err := json.Unmarshal(arr, &elements); err != nil {
			return nil, err
		}
		return elements, nil
	}

	// Bare array
	var elements []json.RawMessage
	if err := json.Unmarshal(body, &elements); err != nil {
		return nil, err
	}
	return elements, nil
}

// cacheResponseElements parses the upstream response, matches each response
// element to its request element (by index), and caches each element individually.
func cacheResponseElements(
	r *http.Request,
	c Cache, respBody []byte,
	entries []elementEntry,
	bcfg *batchEndpointConfig,
	tier TierConfig, cfg *config.CacheConfig,
) {
	respElements, err := parseBatchResponseElements(respBody, bcfg)
	if err != nil {
		slog.Warn("batch cache: failed to parse response elements", "error", err)
		return
	}

	ttl := resolveTTL(r, tier, cfg)
	requestID := RequestIDFromContext(r.Context())

	for i, respElem := range respElements {
		if i >= len(entries) || entries[i].key == "" {
			continue
		}

		// Only cache elements with a successful status
		if !isSuccessElement(respElem, bcfg) {
			continue
		}

		elemBody, err := json.Marshal(respElem)
		if err != nil {
			continue
		}

		cached := &CachedResponse{
			StatusCode:      200,
			Headers:         http.Header{"Content-Type": {"application/json"}},
			Body:            elemBody,
			CachedAt:        time.Now(),
			TTL:             ttl,
			SourceRequestID: requestID,
		}
		_ = c.Set(r.Context(), entries[i].key, cached, ttl)
	}
}

// parseBatchResponseElements extracts individual response elements from the
// upstream batch response body.
func parseBatchResponseElements(body []byte, bcfg *batchEndpointConfig) ([]json.RawMessage, error) {
	if bcfg.format == batchFormatWrapped {
		var wrapper map[string]json.RawMessage
		if err := json.Unmarshal(body, &wrapper); err != nil {
			return nil, err
		}
		field := bcfg.wrapResp
		arr, ok := wrapper[field]
		if !ok {
			return nil, fmt.Errorf("missing field %q in batch response", field)
		}
		var elements []json.RawMessage
		if err := json.Unmarshal(arr, &elements); err != nil {
			return nil, err
		}
		return elements, nil
	}

	var elements []json.RawMessage
	if err := json.Unmarshal(body, &elements); err != nil {
		return nil, err
	}
	return elements, nil
}

// isSuccessElement checks whether a response element indicates success.
// For wrapped formats (pricing): status.statusCode == 200.
// For bare formats (fees): Status == "Success".
func isSuccessElement(elem json.RawMessage, bcfg *batchEndpointConfig) bool {
	if bcfg.format == batchFormatWrapped {
		var e struct {
			Status struct {
				StatusCode int `json:"statusCode"`
			} `json:"status"`
		}
		if json.Unmarshal(elem, &e) != nil {
			return false
		}
		return e.Status.StatusCode >= 200 && e.Status.StatusCode < 300
	}

	// Bare (fees)
	var e struct {
		Status string `json:"Status"`
	}
	if json.Unmarshal(elem, &e) != nil {
		return false
	}
	return e.Status == "Success"
}

// assembleBatchResponse reconstructs a full batch response from individually
// cached elements.
func assembleBatchResponse(entries []elementEntry, bcfg *batchEndpointConfig) ([]byte, error) {
	elements := make([]json.RawMessage, len(entries))
	for i, e := range entries {
		if e.cached == nil {
			return nil, fmt.Errorf("element %d not cached", i)
		}
		elements[i] = json.RawMessage(e.cached.Body)
	}

	if bcfg.format == batchFormatWrapped {
		wrapper := map[string]any{
			bcfg.wrapResp: elements,
		}
		return json.Marshal(wrapper)
	}

	return json.Marshal(elements)
}

// elementEntry is used internally by handleBatchCache to track per-element state.
type elementEntry struct {
	raw    json.RawMessage
	key    string
	cached *CachedResponse
}
