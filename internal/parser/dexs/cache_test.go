package dex

import (
	"sync"
	"testing"
	"time"

	"unified-tx-parser/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- CacheManager[string] tests ---

func TestCacheManager_SetAndGet(t *testing.T) {
	cache := NewCacheManager[string](5 * time.Minute)

	cache.Set("key1", "value1")
	val, ok := cache.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", val)
}

func TestCacheManager_GetMissing(t *testing.T) {
	cache := NewCacheManager[string](5 * time.Minute)

	val, ok := cache.Get("nonexistent")
	assert.False(t, ok)
	assert.Equal(t, "", val) // zero value for string
}

func TestCacheManager_GetExpired(t *testing.T) {
	cache := NewCacheManager[string](1 * time.Millisecond)

	cache.Set("key1", "value1")
	time.Sleep(5 * time.Millisecond)

	val, ok := cache.Get("key1")
	assert.False(t, ok, "expired entry should not be returned")
	assert.Equal(t, "", val)
}

func TestCacheManager_GetOrDefault(t *testing.T) {
	cache := NewCacheManager[string](5 * time.Minute)

	// Missing key returns default
	assert.Equal(t, "fallback", cache.GetOrDefault("missing", "fallback"))

	// Existing key returns stored value
	cache.Set("key1", "value1")
	assert.Equal(t, "value1", cache.GetOrDefault("key1", "fallback"))
}

func TestCacheManager_GetOrDefault_Expired(t *testing.T) {
	cache := NewCacheManager[string](1 * time.Millisecond)

	cache.Set("key1", "value1")
	time.Sleep(5 * time.Millisecond)

	assert.Equal(t, "default", cache.GetOrDefault("key1", "default"))
}

func TestCacheManager_Delete(t *testing.T) {
	cache := NewCacheManager[string](5 * time.Minute)

	cache.Set("key1", "value1")
	cache.Delete("key1")

	_, ok := cache.Get("key1")
	assert.False(t, ok)
}

func TestCacheManager_Delete_NonExistent(t *testing.T) {
	cache := NewCacheManager[string](5 * time.Minute)
	cache.Delete("nonexistent") // should not panic
}

func TestCacheManager_Clear(t *testing.T) {
	cache := NewCacheManager[string](5 * time.Minute)

	cache.Set("key1", "value1")
	cache.Set("key2", "value2")
	cache.Set("key3", "value3")

	assert.Equal(t, 3, cache.Size())
	cache.Clear()
	assert.Equal(t, 0, cache.Size())
}

func TestCacheManager_Size(t *testing.T) {
	cache := NewCacheManager[int](5 * time.Minute)

	assert.Equal(t, 0, cache.Size())
	cache.Set("a", 1)
	assert.Equal(t, 1, cache.Size())
	cache.Set("b", 2)
	assert.Equal(t, 2, cache.Size())
	cache.Delete("a")
	assert.Equal(t, 1, cache.Size())
}

func TestCacheManager_Exists(t *testing.T) {
	cache := NewCacheManager[string](5 * time.Minute)

	cache.Set("key1", "value1")
	assert.True(t, cache.Exists("key1"))
	assert.False(t, cache.Exists("key2"))
}

func TestCacheManager_Exists_Expired(t *testing.T) {
	cache := NewCacheManager[string](1 * time.Millisecond)

	cache.Set("key1", "value1")
	time.Sleep(5 * time.Millisecond)

	assert.False(t, cache.Exists("key1"))
}

func TestCacheManager_Overwrite(t *testing.T) {
	cache := NewCacheManager[string](5 * time.Minute)

	cache.Set("key1", "old")
	cache.Set("key1", "new")

	val, ok := cache.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "new", val)
	assert.Equal(t, 1, cache.Size())
}

func TestCacheManager_Cleanup(t *testing.T) {
	cache := NewCacheManager[string](1 * time.Millisecond)

	cache.Set("key1", "value1")
	cache.Set("key2", "value2")
	time.Sleep(5 * time.Millisecond)

	// Add a non-expired entry
	cache.Set("key3", "value3")

	cache.Cleanup()

	assert.Equal(t, 1, cache.Size())
	assert.True(t, cache.Exists("key3"))
	assert.False(t, cache.Exists("key1"))
	assert.False(t, cache.Exists("key2"))
}

func TestCacheManager_Cleanup_OnExpireCallback(t *testing.T) {
	cache := NewCacheManager[string](1 * time.Millisecond)
	expiredKeys := make([]string, 0)
	cache.onExpire = func(key string, value *CacheEntry[string]) {
		expiredKeys = append(expiredKeys, key)
	}

	cache.Set("a", "1")
	cache.Set("b", "2")
	time.Sleep(5 * time.Millisecond)

	cache.Cleanup()

	assert.Len(t, expiredKeys, 2)
	assert.Contains(t, expiredKeys, "a")
	assert.Contains(t, expiredKeys, "b")
}

func TestCacheManager_ConcurrentAccess(t *testing.T) {
	cache := NewCacheManager[int](5 * time.Minute)
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cache.Set("key", idx)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache.Get("key")
		}()
	}

	wg.Wait()

	// Should have exactly one entry
	assert.Equal(t, 1, cache.Size())
	_, ok := cache.Get("key")
	assert.True(t, ok)
}

// --- TokenCache tests ---

func TestNewTokenCache(t *testing.T) {
	tc := NewTokenCache(10 * time.Minute)
	require.NotNil(t, tc)
	require.NotNil(t, tc.CacheManager)
	assert.Equal(t, 0, tc.Size())
}

func TestTokenCache_SetAndGet(t *testing.T) {
	tc := NewTokenCache(5 * time.Minute)

	token := model.Token{
		Addr:     "0xUSDT",
		Symbol:   "USDT",
		Decimals: 6,
	}
	tc.Set("0xUSDT", token)

	val, ok := tc.Get("0xUSDT")
	assert.True(t, ok)
	assert.Equal(t, "USDT", val.Symbol)
	assert.Equal(t, 6, val.Decimals)
}

func TestTokenCache_Expiration(t *testing.T) {
	tc := NewTokenCache(1 * time.Millisecond)

	tc.Set("0xUSDT", model.Token{Symbol: "USDT"})
	time.Sleep(5 * time.Millisecond)

	_, ok := tc.Get("0xUSDT")
	assert.False(t, ok)
}

// --- PoolObjectCache tests ---

func TestNewPoolObjectCache(t *testing.T) {
	pc := NewPoolObjectCache(10 * time.Minute)
	require.NotNil(t, pc)
	assert.Equal(t, 0, pc.Size())
}

// --- BatchTokenCache tests ---

func TestNewBatchTokenCache(t *testing.T) {
	btc := NewBatchTokenCache()
	require.NotNil(t, btc)
	assert.Equal(t, 0, btc.Size())
}

func TestBatchTokenCache_GetOrCreateCache(t *testing.T) {
	btc := NewBatchTokenCache()

	cache1 := btc.GetOrCreateCache("0xUSDT", 5*time.Minute)
	require.NotNil(t, cache1)

	// Same key returns the same cache instance
	cache2 := btc.GetOrCreateCache("0xUSDT", 10*time.Minute)
	assert.Same(t, cache1, cache2, "same key should return same cache pointer")

	// Different key creates a different cache
	cache3 := btc.GetOrCreateCache("0xWBNB", 5*time.Minute)
	assert.NotSame(t, cache1, cache3, "different key should return different cache pointer")
}

func TestBatchTokenCache_GetCache(t *testing.T) {
	btc := NewBatchTokenCache()

	// Non-existent
	_, ok := btc.GetCache("0xMissing")
	assert.False(t, ok)

	// After creating
	btc.GetOrCreateCache("0xUSDT", 5*time.Minute)
	cache, ok := btc.GetCache("0xUSDT")
	assert.True(t, ok)
	assert.NotNil(t, cache)
}

func TestBatchTokenCache_Size(t *testing.T) {
	btc := NewBatchTokenCache()

	cache1 := btc.GetOrCreateCache("0xUSDT", 5*time.Minute)
	cache1.Set("token1", model.Token{Symbol: "T1"})
	cache1.Set("token2", model.Token{Symbol: "T2"})

	cache2 := btc.GetOrCreateCache("0xWBNB", 5*time.Minute)
	cache2.Set("token3", model.Token{Symbol: "T3"})

	assert.Equal(t, 3, btc.Size())
}

func TestBatchTokenCache_ClearAll(t *testing.T) {
	btc := NewBatchTokenCache()

	cache1 := btc.GetOrCreateCache("0xUSDT", 5*time.Minute)
	cache1.Set("token1", model.Token{Symbol: "T1"})

	cache2 := btc.GetOrCreateCache("0xWBNB", 5*time.Minute)
	cache2.Set("token2", model.Token{Symbol: "T2"})

	btc.ClearAll()
	assert.Equal(t, 0, btc.Size())

	// Caches should be gone
	_, ok := btc.GetCache("0xUSDT")
	assert.False(t, ok)
}

func TestBatchTokenCache_CleanupAll(t *testing.T) {
	btc := NewBatchTokenCache()

	cache1 := btc.GetOrCreateCache("0xUSDT", 1*time.Millisecond)
	cache1.Set("expired", model.Token{Symbol: "OLD"})
	time.Sleep(5 * time.Millisecond)

	cache2 := btc.GetOrCreateCache("0xWBNB", 5*time.Minute)
	cache2.Set("fresh", model.Token{Symbol: "NEW"})

	btc.CleanupAll()

	// Expired entry should be cleaned
	assert.False(t, cache1.Exists("expired"))
	// Fresh entry should remain
	assert.True(t, cache2.Exists("fresh"))
}
