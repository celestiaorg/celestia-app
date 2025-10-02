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
	// this is what I got in tests for signed and encoded PFB tx
	pfbSize := 338
	testCases := []struct {
		name   string
		numTxs int
	}{
		{"1000_txs", 1000},
		{"2000_txs", 2000},
		// it seems this should be a realistic number given the fact that we would have max 2000
		// or less txs per block and around 2 block wait until the transaction is included
		{"4000_txs", 4000},
		{"10000_txs", 10000},
	}

	b.Run("Set", func(b *testing.B) {
		for _, tc := range testCases {
			b.Run(tc.name, func(b *testing.B) {
				txs := generateRandomTxs(tc.numTxs, pfbSize)
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
				txs := generateRandomTxs(tc.numTxs, pfbSize)
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
				txs := generateRandomTxs(tc.numTxs, pfbSize)
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

	b.Run("MemoryUsage", func(b *testing.B) {
		for _, tc := range testCases {
			b.Run(tc.name, func(b *testing.B) {
				b.ReportAllocs()

				for b.Loop() {
					cache := NewTxValidationCache()
					txs := generateRandomTxs(tc.numTxs, pfbSize)

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
