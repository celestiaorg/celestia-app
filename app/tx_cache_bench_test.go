package app

import (
	"math/rand"
	"testing"

	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
)

// generateRandomTxs creates a slice of random transaction bytes for testing
func generateRandomTxsWithRandomSize(count int, size int) [][]byte {
	txs := make([][]byte, count)
	for i := 0; i < count; i++ {
		tx := make([]byte, rand.Intn(size))
		rand.Read(tx)
		txs[i] = tx
	}
	return txs
}

func generateRandomTxs(count int, size int) [][]byte {
	txs := make([][]byte, count)
	for i := 0; i < count; i++ {
		tx := make([]byte, size)
		rand.Read(tx)
		txs[i] = tx
	}
	return txs
}

// populateCache fills the cache with the given transactions
func populateCache(cache *TxValidationCache, txs [][]byte) {
	for i, tx := range txs {
		// Alternate between valid and invalid to simulate real usage
		cache.Set(tx, i%2 == 0)
	}
}

func BenchmarkTxValidationCache_Set(b *testing.B) {
	cache := NewTxValidationCache()
	txs := generateRandomTxs(500, appconsts.MaxTxSize)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		b.StartTimer()
		populateCache(cache, txs)
		b.StopTimer()
	}

}

// BenchmarkTxValidationCache_Clear benchmarks clearing the entire cache
func BenchmarkTxValidationCache_Clear(b *testing.B) {
	testCases := []struct {
		name   string
		numTxs int
		txSize int
	}{
		{"500_small_txs", 500, 1024},
		{"50_txs_max_tx_size", 50, appconsts.MaxTxSize},   // 50 * 8MiB = ~400MB
		{"100_txs_max_tx_size", 120, appconsts.MaxTxSize}, // 120 * 8MiB = ~1GB
		{"500_txs_max_tx_size", 500, appconsts.MaxTxSize}, // 500 * 8MiB = ~4GB
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			// Generate data once per sub-benchmark
			txs := generateRandomTxs(tc.numTxs, tc.txSize)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				// Create and populate fresh cache each iteration
				cache := NewTxValidationCache()
				populateCache(cache, txs)
				b.StartTimer()

				cache.Clear() // Measure only this
			}
		})
	}
}

// BenchmarkTxValidationCache_Cleanup benchmarks the cleanup operation
func BenchmarkTxValidationCache_Cleanup(b *testing.B) {
	testCases := []struct {
		name   string
		numTxs int
		txSize int
	}{
		{"1000_txs_1KB", 1000, 1024},
		{"500_txs_8KB", 500, 8192},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			txs := generateRandomTxsWithRandomSize(tc.numTxs, tc.txSize)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				cache := NewTxValidationCache()
				populateCache(cache, txs)
				b.StartTimer()

				cache.Cleanup()
			}
		})
	}
}

// BenchmarkTxValidationCache_RemoveTransactions benchmarks selective removal
func BenchmarkTxValidationCache_RemoveTransactions(b *testing.B) {
	testCases := []struct {
		name      string
		totalTxs  int
		removeTxs int
		txSize    int
	}{
		{"remove_50_from_500", 500, 50, appconsts.MaxTxSize},
		{"remove_100_from_500", 500, 100, appconsts.MaxTxSize / 2},
		{"remove_250_from_500", 500, 250, appconsts.MaxTxSize / 4},
		{"remove_500_from_500", 500, 500, appconsts.MaxTxSize / 8},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			allTxs := generateRandomTxs(tc.totalTxs, tc.txSize)
			toRemove := allTxs[:tc.removeTxs] // Remove first N transactions

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				cache := NewTxValidationCache()
				populateCache(cache, allTxs)
				b.StartTimer()

				for _, tx := range toRemove {
					cache.RemoveTransaction(tx)
				}
			}
		})
	}
}

// BenchmarkTxValidationCache_MemoryUsage benchmarks memory allocation patterns
func BenchmarkTxValidationCache_MemoryUsage(b *testing.B) {
	testCases := []struct {
		name   string
		numTxs int
		txSize int
	}{
		{"500_txs_8MiB", 500, appconsts.MaxTxSize},
		{"500_txs_4MiB", 500, appconsts.MaxTxSize / 2},
		{"1000_txs_2MiB", 1000, appconsts.MaxTxSize / 4},
		{"100_000_txs_8MiB", 100_000, appconsts.MaxTxSize},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				cache := NewTxValidationCache()
				txs := generateRandomTxs(tc.numTxs, tc.txSize)

				// Populate
				for _, tx := range txs {
					cache.Set(tx, true)
				}

				// Get operations
				for _, tx := range txs {
					cache.Get(tx)
				}

				// Cleanup
				cache.Clear()
			}
		})
	}
}
