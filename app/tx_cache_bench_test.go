package app

import (
	"crypto/rand"
	mathrand "math/rand"
	"testing"

	"github.com/celestiaorg/celestia-app/v8/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v8/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v8/test/util/random"
	"github.com/celestiaorg/go-square/v3/share"
)

func generateRandomTxs(count int, size int) [][]byte {
	txs := make([][]byte, count)
	for i := range count {
		tx := make([]byte, size)
		_, _ = rand.Read(tx)
		txs[i] = tx
	}
	return txs
}

func generateRandomSizedBlobs(count int) [][]*share.Blob {
	blobs := make([][]*share.Blob, count)
	for i := range count {
		blobSize := mathrand.Intn(appconsts.MaxTxSize) + 1
		blobs[i] = blobfactory.ManyRandBlobs(random.New(), blobSize)
	}
	return blobs
}

// populateCache fills the cache with the given transactions
func populateCache(cache *TxCache, txs [][]byte, blobs [][]*share.Blob) {
	for i, tx := range txs {
		cache.Set(tx, blobs[i])
	}
}

// BenchmarkTxCache_Operations benchmarks cache operations
func BenchmarkTxCache_Operations(b *testing.B) {
	// this is the avg size of `Tx` part of `blobTx`
	// which consists of commitments from blob shares,
	// we hash it to use as key for transaction cache
	// the numbers used here are average for block transactions
	txSize := 1130
	testCases := []struct {
		name       string
		numBlobTxs int
	}{
		{"15_txs", 15},
		{"20_txs", 20},
		// this is the max num possible in a block
		{"200_txs", 200},
	}

	b.Run("Set", func(b *testing.B) {
		for _, tc := range testCases {
			b.Run(tc.name, func(b *testing.B) {
				txs := generateRandomTxs(tc.numBlobTxs, txSize)
				b.ResetTimer()
				for b.Loop() {
					b.StopTimer()
					cache := NewTxCache()
					b.StartTimer()

					for _, tx := range txs {
						blobSize := mathrand.Intn(appconsts.MaxTxSize) + 1
						blobs := blobfactory.ManyRandBlobs(random.New(), blobSize)
						cache.Set(tx, blobs)
					}
				}
			})
		}
	})

	b.Run("Exists", func(b *testing.B) {
		for _, tc := range testCases {
			b.Run(tc.name, func(b *testing.B) {
				cache := NewTxCache()
				txs := generateRandomTxs(tc.numBlobTxs, txSize)
				blobs := generateRandomSizedBlobs(tc.numBlobTxs)
				populateCache(cache, txs, blobs)
				b.ResetTimer()

				for b.Loop() {
					for i, tx := range txs {
						cache.Exists(tx, blobs[i])
					}
				}
			})
		}
	})

	b.Run("All operations (set, exists, remove)", func(b *testing.B) {
		for _, tc := range testCases {
			b.Run(tc.name, func(b *testing.B) {
				b.ReportAllocs()

				for b.Loop() {
					cache := NewTxCache()
					txs := generateRandomTxs(tc.numBlobTxs, txSize)
					blobs := generateRandomSizedBlobs(tc.numBlobTxs)
					for i, tx := range txs {
						cache.Set(tx, blobs[i])
					}

					for i, tx := range txs {
						cache.Exists(tx, blobs[i])
					}

					for _, tx := range txs {
						cache.RemoveTransaction(tx)
					}
				}
			})
		}
	})
}
