package app

import (
	"testing"

	blobtx "github.com/celestiaorg/go-square/v4/tx"
	"github.com/stretchr/testify/require"
)

func testProposalKey() proposalKey {
	return proposalKey{chainID: "test", appVersion: 10, height: 42, maxSquareSize: 128}
}

func testArtifacts(txs [][]byte) *proposalArtifacts {
	return &proposalArtifacts{
		key:        testProposalKey(),
		txs:        txs,
		blobTxs:    make([]*blobtx.BlobTx, len(txs)),
		squareSize: 8,
		dataRoot:   []byte("root"),
	}
}

func TestProposalCacheHit(t *testing.T) {
	cache := newProposalCache()
	txs := [][]byte{[]byte("a"), []byte("b")}
	cache.Store(testArtifacts(txs))

	entry := cache.Take(testProposalKey(), txs)
	require.NotNil(t, entry)
	require.Equal(t, uint64(8), entry.squareSize)
}

func TestProposalCacheTakeEmpties(t *testing.T) {
	cache := newProposalCache()
	txs := [][]byte{[]byte("a")}
	cache.Store(testArtifacts(txs))

	require.NotNil(t, cache.Take(testProposalKey(), txs))
	require.Nil(t, cache.Take(testProposalKey(), txs))
}

func TestProposalCacheMissOnDifferentKey(t *testing.T) {
	txs := [][]byte{[]byte("a")}
	keys := map[string]proposalKey{
		"height":          {chainID: "test", appVersion: 10, height: 43, maxSquareSize: 128},
		"chain id":        {chainID: "other", appVersion: 10, height: 42, maxSquareSize: 128},
		"app version":     {chainID: "test", appVersion: 11, height: 42, maxSquareSize: 128},
		"max square size": {chainID: "test", appVersion: 10, height: 42, maxSquareSize: 64},
	}
	for name, key := range keys {
		t.Run(name, func(t *testing.T) {
			cache := newProposalCache()
			cache.Store(testArtifacts(txs))
			require.Nil(t, cache.Take(key, txs))
		})
	}
}

func TestProposalCacheMissOnDifferentTxs(t *testing.T) {
	cache := newProposalCache()
	cache.Store(testArtifacts([][]byte{[]byte("a"), []byte("b")}))

	require.Nil(t, cache.Take(testProposalKey(), [][]byte{[]byte("a"), []byte("c")}))
}

// TestProposalCacheMissOnMutatedTxs covers the reason the cache snapshots the
// tx bytes: without a private copy, rewriting the caller's buffer would compare
// the new bytes against themselves and hit.
func TestProposalCacheMissOnMutatedTxs(t *testing.T) {
	cache := newProposalCache()
	txs := [][]byte{[]byte("a"), []byte("b")}
	cache.Store(testArtifacts(txs))

	txs[1][0] = 'c'
	require.Nil(t, cache.Take(testProposalKey(), txs))
}
