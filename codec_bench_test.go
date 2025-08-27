package rsema1d

import (
	"crypto/rand"
	"fmt"
	"runtime"
	"testing"
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
		rand.Read(data[i])
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
func runBenchmark(b *testing.B, cfg benchmarkConfig, setup func() any, benchFunc func(any) error) {
	b.SetBytes(int64(cfg.dataSize.bytes))
	
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
				_, _, err := Encode(data, codecConfig)
				return err
			}

			// Run benchmark with the new pattern
			runBenchmark(b, cfg, setup, benchFunc)
		})
	}
}
