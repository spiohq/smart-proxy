package cache

import (
	"net/url"
	"sort"
	"strings"
)

// GenerateCacheKey creates a deterministic, human-readable cache key.
// Format: "merchantKey:method:path?sortedQuery[:customSuffix]"
// Keys are NOT hashed  -  this allows prefix-based invalidation via DeleteByPrefix.
// Query parameters are sorted alphabetically so "A=1&B=2" == "B=2&A=1".
func GenerateCacheKey(merchantKey, method, path, query, customSuffix string) string {
	key := merchantKey + ":" + method + ":" + path
	if sorted := SortQueryParams(query); sorted != "" {
		key += "?" + sorted
	}
	if customSuffix != "" {
		key += ":" + customSuffix
	}
	return key
}

// SortQueryParams sorts query parameters alphabetically for deterministic keys.
func SortQueryParams(query string) string {
	if query == "" {
		return ""
	}
	params, err := url.ParseQuery(query)
	if err != nil {
		return query
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		vals := params[k]
		sort.Strings(vals)
		for _, v := range vals {
			parts = append(parts, k+"="+v)
		}
	}
	return strings.Join(parts, "&")
}
