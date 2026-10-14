package app

import (
	"encoding/binary"
	"testing"

	fibreante "github.com/celestiaorg/celestia-app/v10/x/fibre/ante"
	"github.com/stretchr/testify/require"
)

func TestPffSigVerificationCacheEvictsLeastRecentlyUsed(t *testing.T) {
	key := func(i int) fibreante.PffSigCacheKey {
		var k fibreante.PffSigCacheKey
		binary.BigEndian.PutUint64(k[:], uint64(i))
		return k
	}
	cache := NewPffSigVerificationCache()
	require.False(t, cache.IsCached(key(0)))

	for i := range defaultPffSigVerificationCacheCapacity {
		cache.Cache(key(i))
	}
	// A lookup refreshes key 0, so key 1 is now the least recently used.
	require.True(t, cache.IsCached(key(0)))
	cache.Cache(key(defaultPffSigVerificationCacheCapacity))

	require.False(t, cache.IsCached(key(1)), "least recently used key must be evicted")
	require.True(t, cache.IsCached(key(0)))
	require.True(t, cache.IsCached(key(2)))
	require.True(t, cache.IsCached(key(defaultPffSigVerificationCacheCapacity)))
}
