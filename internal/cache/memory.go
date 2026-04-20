package cache

import (
	"container/list"
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MemoryCache is an in-memory LRU cache with background expiration.
type MemoryCache struct {
	mu        sync.RWMutex
	entries   map[string]*list.Element
	lruList   *list.List
	maxBytes  int64
	usedBytes int64
	hits      atomic.Int64
	misses    atomic.Int64
	evictions atomic.Int64
	stopCh    chan struct{}
}

type cacheEntry struct {
	key       string
	response  *CachedResponse
	expiresAt time.Time
	size      int64
}

// NewMemoryCache creates an in-memory cache with LRU eviction.
// maxBytes sets the memory limit; use 0 for unlimited (not recommended).
func NewMemoryCache(maxBytes int64) *MemoryCache {
	mc := &MemoryCache{
		entries:  make(map[string]*list.Element),
		lruList:  list.New(),
		maxBytes: maxBytes,
		stopCh:   make(chan struct{}),
	}
	go mc.evictionLoop()
	return mc
}

func (mc *MemoryCache) Get(_ context.Context, key string) (*CachedResponse, error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	elem, ok := mc.entries[key]
	if !ok {
		mc.misses.Add(1)
		return nil, nil
	}

	entry := elem.Value.(*cacheEntry)
	if time.Now().After(entry.expiresAt) {
		mc.removeLocked(elem)
		mc.misses.Add(1)
		return nil, nil
	}

	mc.lruList.MoveToFront(elem)
	mc.hits.Add(1)
	return entry.response, nil
}

func (mc *MemoryCache) Set(_ context.Context, key string, resp *CachedResponse, ttl time.Duration) error {
	size := entrySize(resp)

	mc.mu.Lock()
	defer mc.mu.Unlock()

	if elem, ok := mc.entries[key]; ok {
		mc.removeLocked(elem)
	}

	for mc.maxBytes > 0 && mc.usedBytes+size > mc.maxBytes && mc.lruList.Len() > 0 {
		mc.evictLRULocked()
	}

	entry := &cacheEntry{
		key:       key,
		response:  resp,
		expiresAt: time.Now().Add(ttl),
		size:      size,
	}
	elem := mc.lruList.PushFront(entry)
	mc.entries[key] = elem
	mc.usedBytes += size
	return nil
}

func (mc *MemoryCache) Delete(_ context.Context, key string) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if elem, ok := mc.entries[key]; ok {
		mc.removeLocked(elem)
	}
	return nil
}

func (mc *MemoryCache) DeleteByPrefix(_ context.Context, prefix string) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	var toDelete []*list.Element
	for key, elem := range mc.entries {
		if strings.HasPrefix(key, prefix) {
			toDelete = append(toDelete, elem)
		}
	}
	for _, elem := range toDelete {
		mc.removeLocked(elem)
	}
	return nil
}

func (mc *MemoryCache) Stats() CacheStats {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	return CacheStats{
		Hits:       mc.hits.Load(),
		Misses:     mc.misses.Load(),
		Evictions:  mc.evictions.Load(),
		BytesUsed:  mc.usedBytes,
		EntryCount: int64(len(mc.entries)),
	}
}

func (mc *MemoryCache) Close() error {
	close(mc.stopCh)
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.entries = make(map[string]*list.Element)
	mc.lruList.Init()
	mc.usedBytes = 0
	return nil
}

func (mc *MemoryCache) removeLocked(elem *list.Element) {
	entry := mc.lruList.Remove(elem).(*cacheEntry)
	delete(mc.entries, entry.key)
	mc.usedBytes -= entry.size
}

func (mc *MemoryCache) evictLRULocked() {
	back := mc.lruList.Back()
	if back == nil {
		return
	}
	mc.removeLocked(back)
	mc.evictions.Add(1)
}

func (mc *MemoryCache) evictionLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-mc.stopCh:
			return
		case <-ticker.C:
			mc.evictExpired()
		}
	}
}

func (mc *MemoryCache) evictExpired() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	now := time.Now()
	var expired []*list.Element
	for _, elem := range mc.entries {
		entry := elem.Value.(*cacheEntry)
		if now.After(entry.expiresAt) {
			expired = append(expired, elem)
		}
	}
	for _, elem := range expired {
		mc.removeLocked(elem)
		mc.evictions.Add(1)
	}

	threshold := int64(float64(mc.maxBytes) * 0.8)
	for mc.maxBytes > 0 && mc.usedBytes > threshold && mc.lruList.Len() > 0 {
		mc.evictLRULocked()
	}
}

func entrySize(resp *CachedResponse) int64 {
	size := int64(len(resp.Body))
	for k, vals := range resp.Headers {
		size += int64(len(k))
		for _, v := range vals {
			size += int64(len(v))
		}
	}
	size += 128 // struct overhead estimate
	return size
}
