package variants

import (
	"sync"
	"time"
)

// ttlCache is a simple in-memory cache with per-entry TTL.
type ttlCache[V any] struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry[V]
	ttl     time.Duration
}

type cacheEntry[V any] struct {
	value   V
	expires time.Time
}

// newTTLCache creates a cache with the given TTL.
func newTTLCache[V any](ttl time.Duration) *ttlCache[V] {
	return &ttlCache[V]{
		entries: make(map[string]cacheEntry[V]),
		ttl:     ttl,
	}
}

// Get returns a cached value and true if it is still fresh.
func (c *ttlCache[V]) Get(key string) (V, bool) {
	c.mu.RLock()
	ent, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok || time.Now().After(ent.expires) {
		var zero V
		return zero, false
	}
	return ent.value, true
}

// Set stores a value with the configured TTL.
func (c *ttlCache[V]) Set(key string, value V) {
	c.mu.Lock()
	c.entries[key] = cacheEntry[V]{value: value, expires: time.Now().Add(c.ttl)}
	c.mu.Unlock()
}

// Evict removes a key from the cache.
func (c *ttlCache[V]) Evict(key string) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}
