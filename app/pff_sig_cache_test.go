package app

import (
	"sync"
	"testing"

	fibreante "github.com/celestiaorg/celestia-app/v10/x/fibre/ante"
	"github.com/stretchr/testify/require"
)

func TestPffSigVerificationCache(t *testing.T) {
	cache := newPffSigVerificationCache(2)
	first := pffCacheTestKey(1)
	second := pffCacheTestKey(2)
	third := pffCacheTestKey(3)

	require.False(t, cache.IsCached(first))
	cache.Cache(first)
	cache.Cache(second)
	require.True(t, cache.IsCached(first)) // first is now most recently used

	cache.Cache(third)
	require.True(t, cache.IsCached(first))
	require.False(t, cache.IsCached(second))
	require.True(t, cache.IsCached(third))
	require.Len(t, cache.entries, 2)
}

func TestPffSigVerificationCacheRecachingDoesNotEvict(t *testing.T) {
	cache := newPffSigVerificationCache(1)
	key := pffCacheTestKey(1)

	cache.Cache(key)
	cache.Cache(key)

	require.True(t, cache.IsCached(key))
	require.Len(t, cache.entries, 1)
}

func TestPffSigVerificationCacheConcurrentAccessRemainsBounded(t *testing.T) {
	const capacity = 32
	cache := newPffSigVerificationCache(capacity)

	var wg sync.WaitGroup
	for i := 0; i < 1_000; i++ {
		key := pffCacheTestKey(i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache.Cache(key)
			cache.IsCached(key)
		}()
	}
	wg.Wait()

	require.LessOrEqual(t, len(cache.entries), capacity)
	require.Equal(t, len(cache.entries), cache.lru.Len())
}

func TestNewPffSigVerificationCacheRejectsNonPositiveCapacity(t *testing.T) {
	require.Panics(t, func() { newPffSigVerificationCache(0) })
}

func pffCacheTestKey(value int) fibreante.PffSigCacheKey {
	var key fibreante.PffSigCacheKey
	key[0] = byte(value)
	key[1] = byte(value >> 8)
	return key
}
