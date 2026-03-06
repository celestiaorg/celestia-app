package rsema1d

import (
	"fmt"
	"math/rand/v2"
	"runtime"
	"testing"

	"github.com/celestiaorg/celestia-app/v8/pkg/rsema1d/field"
)

// dataSize represents a test data size with its human-readable name
type dataSize struct {
	bytes int
	name  string
}

// dataSizes to test
var dataSizes = []dataSize{
	{128 * 1024, "128KB"},
	{1 * 1024 * 1024, "1MB"},
	{8 * 1024 * 1024, "8MB"},
	{16 * 1024 * 1024, "16MB"},
	{64 * 1024 * 1024, "64MB"},
	{128 * 1024 * 1024, "128MB"},
}

// kValues to test
var kValues = []int{
	1024,  // 2^10
	4096,  // 2^12
	16384, // 2^14
}

// encodingRatios - N will be K * ratio
var encodingRatios = []int{
	1, // 1:1 ratio (N = K)
	3, // 1:3 ratio (N = 3K)
}

// workerCounts to test (0 means use runtime.NumCPU())
var workerCounts = []int{
	1, // single worker
	0, // parallel (will use runtime.NumCPU())
}

// parallelModes to test
var parallelModes = []bool{
	false, // sequential execution
	true,  // parallel execution using b.RunParallel
}

// generateTestData creates random test data for benchmarking
func generateTestData(k, rowSize int) [][]byte {
	data := make([][]byte, k)
	for i := range k {
		data[i] = make([]byte, rowSize)
		// Fill with random bytes using math/rand/v2
		for j := range data[i] {
			data[i][j] = byte(rand.IntN(256))
		}
	}
	return data
}

// benchmarkConfig represents a valid benchmark configuration
type benchmarkConfig struct {
	dataSize    dataSize
	k           int
	n           int
	rowSize     int
	workerCount int
	parallel    bool // Use b.RunParallel for concurrent execution
}

// runBenchmark is a helper that handles concurrent benchmark execution
func runBenchmark(b *testing.B, cfg benchmarkConfig, setup func() any, benchFunc func(any) error, reportBytes bool) {
	if reportBytes {
		b.SetBytes(int64(cfg.dataSize.bytes))
	}

	if cfg.parallel {
		// Use b.RunParallel for concurrent execution
		b.RunParallel(func(pb *testing.PB) {
			// Each goroutine creates its own state
			state := setup()

			for pb.Next() {
				if err := benchFunc(state); err != nil {
					b.Errorf("Benchmark failed: %v", err)
				}
			}
		})
	} else {
		// Sequential execution
		state := setup()
		b.ResetTimer() // Reset timer after setup to exclude setup time

		for range b.N {
			if err := benchFunc(state); err != nil {
				b.Fatalf("Benchmark failed: %v", err)
			}
		}
	}
}

// configName generates a descriptive name for a benchmark config
func configName(cfg benchmarkConfig) string {
	name := fmt.Sprintf("size=%s/k=%d/n=%d",
		cfg.dataSize.name, cfg.k, cfg.n)

	// Only include workers if not default single worker
	if cfg.workerCount != 1 {
		name += fmt.Sprintf("/workers=%d", cfg.workerCount)
	}

	// Include parallel mode
	if cfg.parallel {
		name += "/parallel"
	}

	return name
}

// generateBenchmarkConfigs generates valid benchmark configurations
// supportsWorkers indicates if the function being benchmarked supports worker parallelism
func generateBenchmarkConfigs(supportsWorkers bool) []benchmarkConfig {
	var configs []benchmarkConfig

	// Determine which worker counts to use
	workers := []int{1} // Default to single worker
	if supportsWorkers {
		workers = workerCounts
	}

	for _, ds := range dataSizes {
		for _, k := range kValues {
			rowSize := ds.bytes / k
			// Skip invalid row sizes
			if rowSize < 64 || rowSize%64 != 0 {
				continue
			}

			for _, ratio := range encodingRatios {
				n := k * ratio
				// Skip if reaches or exceeds field size limit
				if k+n >= 65536 {
					continue
				}

				for _, w := range workers {
					workerCount := w
					if workerCount == 0 {
						workerCount = runtime.NumCPU()
					}

					// Test both sequential and parallel execution
					for _, parallel := range parallelModes {
						configs = append(configs, benchmarkConfig{
							dataSize:    ds,
							k:           k,
							n:           n,
							rowSize:     rowSize,
							workerCount: workerCount,
							parallel:    parallel,
						})
					}
				}
			}
		}
	}

	return configs
}

// BenchmarkEncode benchmarks the Encode function
func BenchmarkEncode(b *testing.B) {
	// Encode supports worker parallelism
	configs := generateBenchmarkConfigs(true)

	for _, cfg := range configs {
		b.Run(configName(cfg), func(b *testing.B) {
			// Create the codec config once
			codecConfig := &Config{
				K:           cfg.k,
				N:           cfg.n,
				RowSize:     cfg.rowSize,
				WorkerCount: cfg.workerCount,
			}

			// Setup returns the test data
			setup := func() any {
				return generateTestData(cfg.k, cfg.rowSize)
			}

			// Benchmark function receives the test data
			benchFunc := func(state any) error {
				data := state.([][]byte)
				_, _, _, err := Encode(data, codecConfig)
				return err
			}

			// Run benchmark with the new pattern
			runBenchmark(b, cfg, setup, benchFunc, true) // Report MB/s for encoding
		})
	}
}

// BenchmarkReconstruct benchmarks the Reconstruct function
func BenchmarkReconstruct(b *testing.B) {
	// Reconstruct doesn't support worker parallelism directly
	configs := generateBenchmarkConfigs(false)

	// Test different erasure patterns
	erasurePatterns := []struct {
		name       string
		getIndices func(k, n int) []int
	}{
		{
			name: "all_original",
			getIndices: func(k, n int) []int {
				// Use all original rows (fastest case - no actual reconstruction needed)
				return makeRange(0, k)
			},
		},
		{
			name: "all_parity",
			getIndices: func(k, n int) []int {
				// Use only parity rows (requires full reconstruction)
				return makeRange(k, k+k)
			},
		},
		{
			name: "half_half",
			getIndices: func(k, n int) []int {
				// Use half original, half parity
				indices := make([]int, k)
				halfK := k / 2
				for i := range halfK {
					indices[i] = i
				}
				for i := range k - halfK {
					indices[halfK+i] = k + i
				}
				return indices
			},
		},
		{
			name: "random",
			getIndices: func(k, n int) []int {
				// Random K rows from all available rows
				all := make([]int, k+n)
				for i := range all {
					all[i] = i
				}

				// Shuffle using math/rand/v2
				rand.Shuffle(len(all), func(i, j int) {
					all[i], all[j] = all[j], all[i]
				})

				// Take first K indices and sort for consistency
				return all[:k]
			},
		},
	}

	for _, cfg := range configs {
		for _, pattern := range erasurePatterns {
			benchName := fmt.Sprintf("%s/%s", configName(cfg), pattern.name)

			b.Run(benchName, func(b *testing.B) {
				// Create the codec config
				codecConfig := &Config{
					K:           cfg.k,
					N:           cfg.n,
					RowSize:     cfg.rowSize,
					WorkerCount: 1, // Reconstruction uses its own parallelism
				}

				// Setup returns encoded data for reconstruction
				setup := func() any {
					// Generate and encode data once
					originalData := generateTestData(cfg.k, cfg.rowSize)
					extData, _, _, err := Encode(originalData, codecConfig)
					if err != nil {
						b.Fatalf("Encode failed: %v", err)
					}

					// Get indices for this pattern
					indices := pattern.getIndices(cfg.k, cfg.n)

					// Select the rows
					rows := make([][]byte, len(indices))
					for i, idx := range indices {
						rows[i] = extData.rows[idx]
					}

					// Return as a map for clarity
					return map[string]any{
						"rows":    rows,
						"indices": indices,
					}
				}

				// Benchmark function performs reconstruction
				benchFunc := func(state any) error {
					data := state.(map[string]any)
					rows := data["rows"].([][]byte)
					indices := data["indices"].([]int)

					_, err := Reconstruct(rows, indices, codecConfig)
					return err
				}

				// Run the benchmark
				runBenchmark(b, cfg, setup, benchFunc, true) // Report MB/s for reconstruction
			})
		}
	}
}

// BenchmarkGenerateRowProof benchmarks row proof generation
func BenchmarkGenerateRowProof(b *testing.B) {
	configs := generateBenchmarkConfigs(false) // Proof generation doesn't use workers

	for _, cfg := range configs {
		b.Run(configName(cfg), func(b *testing.B) {
			// Create the codec config
			codecConfig := &Config{
				K:           cfg.k,
				N:           cfg.n,
				RowSize:     cfg.rowSize,
				WorkerCount: 1,
			}

			// Setup creates the extended data once
			setup := func() any {
				originalData := generateTestData(cfg.k, cfg.rowSize)
				extData, _, _, err := Encode(originalData, codecConfig)
				if err != nil {
					b.Fatalf("Encode failed: %v", err)
				}
				return extData
			}

			// Benchmark proof generation for index 0
			benchFunc := func(state any) error {
				extData := state.(*ExtendedData)
				_, err := extData.GenerateRowProof(0)
				return err
			}

			runBenchmark(b, cfg, setup, benchFunc, false) // No MB/s for proof operations
		})
	}
}

// BenchmarkGenerateStandaloneProof benchmarks standalone proof generation
func BenchmarkGenerateStandaloneProof(b *testing.B) {
	configs := generateBenchmarkConfigs(false) // Proof generation doesn't use workers

	for _, cfg := range configs {
		b.Run(configName(cfg), func(b *testing.B) {
			// Create the codec config
			codecConfig := &Config{
				K:           cfg.k,
				N:           cfg.n,
				RowSize:     cfg.rowSize,
				WorkerCount: 1,
			}

			// Setup creates the extended data once
			setup := func() any {
				originalData := generateTestData(cfg.k, cfg.rowSize)
				extData, _, _, err := Encode(originalData, codecConfig)
				if err != nil {
					b.Fatalf("Encode failed: %v", err)
				}
				return extData
			}

			// Benchmark standalone proof generation for index 0
			benchFunc := func(state any) error {
				extData := state.(*ExtendedData)
				_, err := extData.GenerateStandaloneProof(0)
				return err
			}

			runBenchmark(b, cfg, setup, benchFunc, false) // No MB/s for proof operations
		})
	}
}

// Benchmark results (AMD Ryzen 9 7940HS) - selected representative cases:
//
// BenchmarkCreateVerificationContext/size=1MB/k=1024/n=1024-16       388,162 ns/op    606,718 B/op     4,121 allocs/op
// BenchmarkCreateVerificationContext/size=1MB/k=4096/n=4096-16     1,742,770 ns/op  2,370,918 B/op    16,411 allocs/op
// BenchmarkCreateVerificationContext/size=1MB/k=16384/n=16384-16   7,763,339 ns/op  9,448,992 B/op    65,565 allocs/op
func BenchmarkCreateVerificationContext(b *testing.B) {
	configs := generateBenchmarkConfigs(false) // Context creation doesn't use workers

	for _, cfg := range configs {
		b.Run(configName(cfg), func(b *testing.B) {
			// Create the codec config
			codecConfig := &Config{
				K:           cfg.k,
				N:           cfg.n,
				RowSize:     cfg.rowSize,
				WorkerCount: 1,
			}

			// Setup prepares the RLC original values
			setup := func() any {
				originalData := generateTestData(cfg.k, cfg.rowSize)
				extData, _, _, err := Encode(originalData, codecConfig)
				if err != nil {
					b.Fatalf("Encode failed: %v", err)
				}
				return extData.rlcOrig
			}

			// Benchmark context creation
			benchFunc := func(state any) error {
				rlcOrig := state.([]field.GF128)
				_, _, err := CreateVerificationContext(rlcOrig, codecConfig)
				return err
			}

			runBenchmark(b, cfg, setup, benchFunc, false) // No MB/s for proof operations
		})
	}
}

// BenchmarkVerifyRowWithContext benchmarks row proof verification with context
func BenchmarkVerifyRowWithContext(b *testing.B) {
	configs := generateBenchmarkConfigs(false) // Verification doesn't use workers

	for _, cfg := range configs {
		b.Run(configName(cfg), func(b *testing.B) {
			// Create the codec config
			codecConfig := &Config{
				K:           cfg.k,
				N:           cfg.n,
				RowSize:     cfg.rowSize,
				WorkerCount: 1,
			}

			// Setup prepares proof and context
			setup := func() any {
				originalData := generateTestData(cfg.k, cfg.rowSize)
				extData, commitment, _, err := Encode(originalData, codecConfig)
				if err != nil {
					b.Fatalf("Encode failed: %v", err)
				}

				// Create verification context
				ctx, _, err := CreateVerificationContext(extData.rlcOrig, codecConfig)
				if err != nil {
					b.Fatalf("CreateVerificationContext failed: %v", err)
				}

				// Pre-generate proof for index 0
				proof, err := extData.GenerateRowProof(0)
				if err != nil {
					b.Fatalf("GenerateRowProof failed: %v", err)
				}

				return map[string]any{
					"proof":      proof,
					"commitment": commitment,
					"context":    ctx,
				}
			}

			// Benchmark verification of the same proof each iteration
			benchFunc := func(state any) error {
				data := state.(map[string]any)
				proof := data["proof"].(*RowProof)
				commitment := data["commitment"].(Commitment)
				ctx := data["context"].(*VerificationContext)

				return VerifyRowWithContext(proof, commitment, ctx)
			}

			runBenchmark(b, cfg, setup, benchFunc, false) // No MB/s for proof operations
		})
	}
}

// BenchmarkVerifyStandaloneProof benchmarks standalone proof verification
func BenchmarkVerifyStandaloneProof(b *testing.B) {
	configs := generateBenchmarkConfigs(false) // Verification doesn't use workers

	for _, cfg := range configs {
		b.Run(configName(cfg), func(b *testing.B) {
			// Create the codec config
			codecConfig := &Config{
				K:           cfg.k,
				N:           cfg.n,
				RowSize:     cfg.rowSize,
				WorkerCount: 1,
			}

			// Setup prepares standalone proof
			setup := func() any {
				originalData := generateTestData(cfg.k, cfg.rowSize)
				extData, commitment, _, err := Encode(originalData, codecConfig)
				if err != nil {
					b.Fatalf("Encode failed: %v", err)
				}

				// Pre-generate standalone proof for index 0
				proof, err := extData.GenerateStandaloneProof(0)
				if err != nil {
					b.Fatalf("GenerateStandaloneProof failed: %v", err)
				}

				return map[string]any{
					"proof":      proof,
					"commitment": commitment,
					"config":     codecConfig,
				}
			}

			// Benchmark verification of the same proof each iteration
			benchFunc := func(state any) error {
				data := state.(map[string]any)
				proof := data["proof"].(*StandaloneProof)
				commitment := data["commitment"].(Commitment)
				config := data["config"].(*Config)

				return VerifyStandaloneProof(proof, commitment, config)
			}

			runBenchmark(b, cfg, setup, benchFunc, false) // No MB/s for proof operations
		})
	}
}
