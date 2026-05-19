package historyserver

import (
	"sync"
	"time"
)

// TTLCache is a simple thread-safe cache with per-entry TTL expiration.
// Each cache instance has its own mutex, so different caches do not contend.
type TTLCache[V any] struct {
	mu    sync.RWMutex
	items map[string]cacheEntry[V]
	ttl   time.Duration
}

type cacheEntry[V any] struct {
	value     V
	expiresAt time.Time
}

// NewTTLCache creates a new TTLCache with the given default TTL.
func NewTTLCache[V any](ttl time.Duration) *TTLCache[V] {
	return &TTLCache[V]{
		items: make(map[string]cacheEntry[V]),
		ttl:   ttl,
	}
}

// Get returns the cached value for the given key, and true if it exists and has not expired.
// Expired entries are not evicted; the key space is small and skipping eviction
// avoids a write lock and a TOCTOU race with concurrent Set calls.
func (c *TTLCache[V]) Get(key string) (V, bool) {
	c.mu.RLock()
	entry, ok := c.items[key]
	c.mu.RUnlock()
	if ok && time.Now().Before(entry.expiresAt) {
		return entry.value, true
	}
	var zero V
	return zero, false
}

// Set stores a value in the cache with the default TTL.
func (c *TTLCache[V]) Set(key string, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = cacheEntry[V]{
		value:     value,
		expiresAt: time.Now().Add(c.ttl),
	}
}

// SetWithExpiry stores a value with a specific expiry time, useful for testing.
func (c *TTLCache[V]) SetWithExpiry(key string, value V, expiresAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = cacheEntry[V]{
		value:     value,
		expiresAt: expiresAt,
	}
}
