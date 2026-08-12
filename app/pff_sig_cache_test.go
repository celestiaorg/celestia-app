package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPffSigVerificationCache(t *testing.T) {
	cache := NewPffSigVerificationCache()
	tx := []byte("tx")
	otherTx := []byte("other-tx")

	require.False(t, cache.IsCached(tx))

	cache.Cache(tx)
	require.True(t, cache.IsCached(tx))
	require.False(t, cache.IsCached(otherTx))

	cache.Delete(tx)
	require.False(t, cache.IsCached(tx))
}
