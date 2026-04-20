package rdt

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// CacheKey identifies a cached RDT by merchant, operation, and dataElements.
type CacheKey struct {
	MerchantID   string
	GenericPath  string
	DataElements string // sorted, comma-joined (e.g. "buyerInfo,buyerTaxInformation,shippingAddress")
}

// CacheEntry holds a cached RDT and its expiry.
type CacheEntry struct {
	Token     string
	ExpiresAt time.Time
}

// Cache is a thread-safe in-memory RDT token cache.
type Cache struct {
	mu           sync.RWMutex
	entries      map[CacheKey]CacheEntry
	safetyMargin time.Duration
}

// NewCache creates a new RDT cache. safetyMargin is subtracted from the
// token's ExpiresAt when checking validity, so tokens are treated as expired
// before they actually expire upstream.
func NewCache(safetyMargin time.Duration) *Cache {
	return &Cache{
		entries:      make(map[CacheKey]CacheEntry),
		safetyMargin: safetyMargin,
	}
}

// Get returns the cached RDT for the given key, if it exists and has not
// expired (accounting for the safety margin). Returns false on miss or expiry.
func (c *Cache) Get(key CacheKey) (CacheEntry, bool) {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()

	if !ok {
		return CacheEntry{}, false
	}
	if time.Now().Add(c.safetyMargin).After(entry.ExpiresAt) {
		// Expired or within safety margin. Clean up lazily.
		c.mu.Lock()
		delete(c.entries, key)
		c.mu.Unlock()
		return CacheEntry{}, false
	}
	return entry, true
}

// Set stores an RDT in the cache, replacing any existing entry for the key.
func (c *Cache) Set(key CacheKey, entry CacheEntry) {
	c.mu.Lock()
	c.entries[key] = entry
	c.mu.Unlock()
}

// Invalidate removes a cache entry. Used when upstream returns 403 after
// a token swap, indicating the cached RDT is no longer valid.
func (c *Cache) Invalidate(key CacheKey) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

// Size returns the number of entries in the cache.
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// BuildCacheKey creates a CacheKey from a merchant ID and PIIOperation.
// DataElements are sorted to ensure deterministic keys regardless of input order.
func BuildCacheKey(merchantID string, op PIIOperation) CacheKey {
	var dataElems string
	if len(op.DataElements) > 0 {
		sorted := make([]string, len(op.DataElements))
		copy(sorted, op.DataElements)
		sort.Strings(sorted)
		dataElems = strings.Join(sorted, ",")
	}
	return CacheKey{
		MerchantID:   merchantID,
		GenericPath:  op.GenericPath,
		DataElements: dataElems,
	}
}
