package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPFFSignatureVerificationCache(t *testing.T) {
	testApp := &App{}
	tx := []byte("pff-tx-1")
	otherTx := []byte("pff-tx-2")

	require.False(t, testApp.isPFFSigCached(tx))

	testApp.cachePFFSig(tx)
	require.True(t, testApp.isPFFSigCached(tx))
	require.False(t, testApp.isPFFSigCached(otherTx))

	// FinalizeBlock evicts finalized txs from the cache using this key.
	testApp.pffSignatureVerificationCache.Delete(pffSigCacheKey(tx))
	require.False(t, testApp.isPFFSigCached(tx))
}

func TestPFFSigCacheKey(t *testing.T) {
	require.Equal(t, pffSigCacheKey([]byte{0x01}), pffSigCacheKey([]byte{0x01}))
	require.NotEqual(t, pffSigCacheKey([]byte{0x01}), pffSigCacheKey([]byte{0x02}))
}
