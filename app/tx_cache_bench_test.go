package app

import (
	"crypto/rand"
	"testing"
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

// populateCache fills the cache with the given transactions
func populateCache(cache *TxValidationCache, txs [][]byte) {
	for _, tx := range txs {
		cache.Set(tx)
	}
}

// BenchmarkTxValidationCache_Operations benchmarks cache operations
func BenchmarkTxValidationCache_Operations(b *testing.B) {
	// this is the avg size of `Tx` part of `blobTx` which consists commitments from blob shares,
	// we hash it to use as key for transaction cache,
	// I got after checking 30000 blocks,the max was 4450
	txSize := 1130
	testCases := []struct {
		name   string
		numTxs int
	}{
		// this is the max num I got after checking 30000 blocks
		{"15_txs", 15},
		{"20_txs", 20},
		// this is the max num possible in block
		{"200_txs", 200},
	}

	b.Run("Set", func(b *testing.B) {
		for _, tc := range testCases {
			b.Run(tc.name, func(b *testing.B) {
				txs := generateRandomTxs(tc.numTxs, txSize)
				b.ResetTimer()
				for b.Loop() {
					b.StopTimer()
					cache := NewTxValidationCache()
					b.StartTimer()

					for _, tx := range txs {
						cache.Set(tx)
					}
				}
			})
		}
	})

	b.Run("Exists", func(b *testing.B) {
		for _, tc := range testCases {
			b.Run(tc.name, func(b *testing.B) {
				cache := NewTxValidationCache()
				txs := generateRandomTxs(tc.numTxs, txSize)
				populateCache(cache, txs)
				b.ResetTimer()

				for b.Loop() {
					for _, tx := range txs {
						cache.Exists(tx)
					}
				}
			})
		}
	})

	b.Run("RemoveTransactions", func(b *testing.B) {
		for _, tc := range testCases {
			b.Run(tc.name, func(b *testing.B) {
				txs := generateRandomTxs(tc.numTxs, txSize)
				b.ResetTimer()

				for b.Loop() {
					b.StopTimer()
					cache := NewTxValidationCache()
					populateCache(cache, txs)
					b.StartTimer()

					for _, tx := range txs {
						cache.RemoveTransaction(tx)
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
					cache := NewTxValidationCache()
					txs := generateRandomTxs(tc.numTxs, txSize)

					for _, tx := range txs {
						cache.Set(tx)
					}

					for _, tx := range txs {
						cache.Exists(tx)
					}

					for _, tx := range txs {
						cache.RemoveTransaction(tx)
					}
				}
			})
		}
	})
}
