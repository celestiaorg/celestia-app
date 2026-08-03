package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPffSigVerificationCache(t *testing.T) {
	testKeeper := NewKeeper(nil, nil, nil, nil, "")
	tx := []byte("pff-tx-1")
	otherTx := []byte("pff-tx-2")

	require.False(t, testKeeper.IsPffSigVerificationCached(tx))

	testKeeper.CachePffSigVerification(tx)
	require.True(t, testKeeper.IsPffSigVerificationCached(tx))
	require.False(t, testKeeper.IsPffSigVerificationCached(otherTx))

	testKeeper.DeletePffSigVerification(tx)
	require.False(t, testKeeper.IsPffSigVerificationCached(tx))
}

func TestPffSigCacheKey(t *testing.T) {
	require.Equal(t, pffSigCacheKey([]byte{0x01}), pffSigCacheKey([]byte{0x01}))
	require.NotEqual(t, pffSigCacheKey([]byte{0x01}), pffSigCacheKey([]byte{0x02}))
}
