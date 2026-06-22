package dex

import (
	"sync"
	"time"

	"unified-tx-parser/internal/model"

	"github.com/block-vision/sui-go-sdk/models"
)

// CacheManager provides generic caching functionality for DEX extractors
type CacheManager[T any] struct {
	data  map[string]*CacheEntry[T]
	ttl   time.Duration
	mu    sync.RWMutex
	onExpire func(key string, value *CacheEntry[T])
}

// CacheEntry holds a cache value with expiration time
type CacheEntry[T any] struct {
	Value     T
	ExpiresAt time.Time
}

// NewCacheManager creates a new cache manager with the given TTL
func NewCacheManager[T any](ttl time.Duration) *CacheManager[T] {
	return &CacheManager[T]{
		data: make(map[string]*CacheEntry[T]),
		ttl:  ttl,
	}
}

// Set stores a value in the cache
func (c *CacheManager[T]) Set(key string, value T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key] = &CacheEntry[T]{
		Value:     value,
		ExpiresAt: time.Now().Add(c.ttl),
	}
}

// Get retrieves a value from the cache
// Returns (value, true) if found and not expired, (zero, false) otherwise
func (c *CacheManager[T]) Get(key string) (T, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.data[key]
	if !exists {
		var zero T
		return zero, false
	}

	if time.Now().After(entry.ExpiresAt) {
		// Entry has expired, but we don't delete it here to avoid lock contention
		var zero T
		return zero, false
	}

	return entry.Value, true
}

// GetOrDefault retrieves a value or returns the default if not found/expired
func (c *CacheManager[T]) GetOrDefault(key string, defaultValue T) T {
	if value, ok := c.Get(key); ok {
		return value
	}
	return defaultValue
}

// Delete removes a value from the cache
func (c *CacheManager[T]) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, key)
}

// Clear removes all entries from the cache
func (c *CacheManager[T]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = make(map[string]*CacheEntry[T])
}

// Cleanup removes expired entries from the cache
func (c *CacheManager[T]) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, entry := range c.data {
		if now.After(entry.ExpiresAt) {
			if c.onExpire != nil {
				c.onExpire(key, entry)
			}
			delete(c.data, key)
		}
	}
}

// Size returns the number of entries in the cache
func (c *CacheManager[T]) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.data)
}

// Exists checks if a key exists and is not expired
func (c *CacheManager[T]) Exists(key string) bool {
	_, ok := c.Get(key)
	return ok
}

// TokenCache is a specialized cache for token metadata
type TokenCache struct {
	*CacheManager[model.Token]
}

// NewTokenCache creates a new token cache with the given TTL
func NewTokenCache(ttl time.Duration) *TokenCache {
	return &TokenCache{
		CacheManager: NewCacheManager[model.Token](ttl),
	}
}

// PoolObjectCache is a specialized cache for Sui pool objects
type PoolObjectCache struct {
	*CacheManager[models.SuiObjectResponse]
}

// NewPoolObjectCache creates a new pool object cache with the given TTL
func NewPoolObjectCache(ttl time.Duration) *PoolObjectCache {
	return &PoolObjectCache{
		CacheManager: NewCacheManager[models.SuiObjectResponse](ttl),
	}
}

// BatchTokenCache manages multiple token caches by asset type
type BatchTokenCache struct {
	caches map[string]*TokenCache
	mu     sync.RWMutex
}

// NewBatchTokenCache creates a new batch token cache
func NewBatchTokenCache() *BatchTokenCache {
	return &BatchTokenCache{
		caches: make(map[string]*TokenCache),
	}
}

// GetOrCreateCache gets a token cache for the given asset, creating if needed
func (b *BatchTokenCache) GetOrCreateCache(assetAddr string, ttl time.Duration) *TokenCache {
	b.mu.Lock()
	defer b.mu.Unlock()

	if cache, exists := b.caches[assetAddr]; exists {
		return cache
	}

	cache := NewTokenCache(ttl)
	b.caches[assetAddr] = cache
	return cache
}

// GetCache gets an existing token cache
func (b *BatchTokenCache) GetCache(assetAddr string) (*TokenCache, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	cache, exists := b.caches[assetAddr]
	return cache, exists
}

// ClearAll clears all caches
func (b *BatchTokenCache) ClearAll() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, cache := range b.caches {
		cache.Clear()
	}
	b.caches = make(map[string]*TokenCache)
}

// CleanupAll performs cleanup on all caches
func (b *BatchTokenCache) CleanupAll() {
	b.mu.RLock()
	caches := make([]*TokenCache, 0, len(b.caches))
	for _, cache := range b.caches {
		caches = append(caches, cache)
	}
	b.mu.RUnlock()

	for _, cache := range caches {
		cache.Cleanup()
	}
}

// Size returns the total number of cached items
func (b *BatchTokenCache) Size() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	total := 0
	for _, cache := range b.caches {
		total += cache.Size()
	}
	return total
}

// Note: TokenCacheItem and PoolCacheItem are defined in bluefin.go for backward compatibility
// These are legacy structures, but kept for existing code that may reference them
