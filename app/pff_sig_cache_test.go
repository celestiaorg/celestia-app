package app

import (
	"sync"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	fibreante "github.com/celestiaorg/celestia-app/v10/x/fibre/ante"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestPffSigVerificationCacheEvictsLeastRecentlyUsed(t *testing.T) {
	cache := newPffSigVerificationCache(2)
	first := pffCacheTestKey(1)
	second := pffCacheTestKey(2)
	third := pffCacheTestKey(3)

	require.False(t, cache.IsCached(first))
	cache.Cache(first)
	cache.Cache(second)
	require.True(t, cache.IsCached(first)) // the lookup promotes first, leaving second least recently used

	cache.Cache(third) // exceeds capacity, evicting second
	require.True(t, cache.IsCached(first))
	require.False(t, cache.IsCached(second))
	require.True(t, cache.IsCached(third))
	require.Equal(t, 2, cache.entries.Len())
}

func TestPffSigVerificationCacheDelete(t *testing.T) {
	cache := newPffSigVerificationCache(2)
	key := pffCacheTestKey(1)

	cache.Delete(key) // deleting an absent key is a no-op

	cache.Cache(key)
	require.True(t, cache.IsCached(key))

	cache.Delete(key)
	require.False(t, cache.IsCached(key))
}

func TestPffSigVerificationCacheRecachingDoesNotEvict(t *testing.T) {
	cache := newPffSigVerificationCache(1)
	key := pffCacheTestKey(1)

	cache.Cache(key)
	cache.Cache(key)

	require.True(t, cache.IsCached(key))
	require.Equal(t, 1, cache.entries.Len())
}

func TestPffSigVerificationCacheConcurrentAccessRemainsBounded(t *testing.T) {
	const capacity = 32
	cache := newPffSigVerificationCache(capacity)

	var wg sync.WaitGroup
	for i := range 1_000 {
		key := pffCacheTestKey(i)
		wg.Go(func() {
			cache.Cache(key)
			cache.IsCached(key)
		})
	}
	wg.Wait()

	require.LessOrEqual(t, cache.entries.Len(), capacity)
}

func TestNewPffSigVerificationCacheRejectsNonPositiveCapacity(t *testing.T) {
	require.Panics(t, func() { newPffSigVerificationCache(0) })
}

func TestEvictFinalizedPffSigCacheEntry(t *testing.T) {
	encodingConfig := encoding.MakeConfig(ModuleEncodingRegisters...)
	testApp := &App{
		encodingConfig: encodingConfig,
		pffSigCache:    NewPffSigVerificationCache(),
	}
	msg := &fibretypes.MsgPayForFibre{ValidatorSignatures: [][]byte{{1}, {2}}}
	pffTx := encodePffTestTx(t, encodingConfig, msg)
	nonPffTx := encodePffTestTx(t, encodingConfig, msg, msg) // multi-message txs carry no PFF certificate

	key, err := fibreante.NewPffSigCacheKey(msg)
	require.NoError(t, err)
	testApp.pffSigCache.Cache(key)

	testApp.evictFinalizedPffSigCacheEntry([]byte("not a decodable tx"))
	require.True(t, testApp.pffSigCache.IsCached(key))

	testApp.evictFinalizedPffSigCacheEntry(nonPffTx)
	require.True(t, testApp.pffSigCache.IsCached(key))

	testApp.evictFinalizedPffSigCacheEntry(pffTx)
	require.False(t, testApp.pffSigCache.IsCached(key))
}

func encodePffTestTx(t *testing.T, encodingConfig encoding.Config, msgs ...*fibretypes.MsgPayForFibre) []byte {
	t.Helper()
	builder := encodingConfig.TxConfig.NewTxBuilder()
	sdkMsgs := make([]sdk.Msg, len(msgs))
	for i, msg := range msgs {
		sdkMsgs[i] = msg
	}
	require.NoError(t, builder.SetMsgs(sdkMsgs...))
	rawTx, err := encodingConfig.TxConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return rawTx
}

func pffCacheTestKey(value int) fibreante.PffSigCacheKey {
	var key fibreante.PffSigCacheKey
	key[0] = byte(value)
	key[1] = byte(value >> 8)
	return key
}
