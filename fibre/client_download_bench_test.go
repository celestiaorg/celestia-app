package fibre_test

import (
	"context"
	"sync"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/fibre"
	"github.com/stretchr/testify/require"
)

// BenchmarkClient_Download measures the performance of the Download operation
// with mocked validators serving the blob's rows directly. This isolates
// client-side reconstruction performance from network and server overhead.
//
// Run with: go test -bench=BenchmarkClient_Download$ -benchmem -count=1 -run=^$ -timeout=15m
func BenchmarkClient_Download(b *testing.B) {
	benchmarks := []struct {
		name     string
		numBytes int
	}{
		{"128_KiB", 128 * 1024},
		{"1_MiB", 1 * 1024 * 1024},
		{"8_MiB", 8 * 1024 * 1024},
		{"32_MiB", 32 * 1024 * 1024},
		{"128_MiB_max", fibre.DefaultProtocolParams.MaxBlobSize},
	}

	// Subtract the blob header overhead so the encoded blob lands within the
	// named size class instead of spilling into the next one.
	headerLen := fibre.DefaultProtocolParams.MaxBlobSize - fibre.DefaultBlobConfigV0().MaxDataSize

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			t := &testing.T{}
			blob := makeTestBlobV0(t, bm.numBytes-headerLen)

			client := makeTestDownloadClient(t, 100, nil, blob)
			defer func() { _ = client.Stop(context.Background()) }()

			ctx := context.Background()
			id := blob.ID()

			for b.Loop() {
				downloaded, err := client.Download(ctx, id)
				require.NoError(b, err)
				downloaded.Free()
			}

			bytesProcessed := int64(b.N) * int64(bm.numBytes)
			b.ReportMetric(float64(bytesProcessed)/b.Elapsed().Seconds()/(1024*1024), "MiB/s")
		})
	}
}

// BenchmarkClient_Download_Concurrent measures concurrent Download operations
// across different blob sizes.
//
// Run with: go test -bench=BenchmarkClient_Download_Concurrent -benchmem -count=1 -run=^$ -timeout=30m
func BenchmarkClient_Download_Concurrent(b *testing.B) {
	benchmarks := []struct {
		name        string
		blobSize    int
		concurrency int
	}{
		{"128_KiB/concurrency_4", 128 * 1024, 4},
		{"128_KiB/concurrency_8", 128 * 1024, 8},
		{"1_MiB/concurrency_4", 1 * 1024 * 1024, 4},
		{"1_MiB/concurrency_8", 1 * 1024 * 1024, 8},
		{"8_MiB/concurrency_4", 8 * 1024 * 1024, 4},
		{"32_MiB/concurrency_4", 32 * 1024 * 1024, 4},
		{"128_MiB_max/concurrency_4", fibre.DefaultProtocolParams.MaxBlobSize, 4},
	}

	headerLen := fibre.DefaultProtocolParams.MaxBlobSize - fibre.DefaultBlobConfigV0().MaxDataSize

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			t := &testing.T{}
			blob := makeTestBlobV0(t, bm.blobSize-headerLen)

			client := makeTestDownloadClient(t, 100, nil, blob)
			defer func() { _ = client.Stop(context.Background()) }()

			ctx := context.Background()
			id := blob.ID()

			for b.Loop() {
				var wg sync.WaitGroup
				wg.Add(bm.concurrency)
				for range bm.concurrency {
					go func() {
						defer wg.Done()
						downloaded, err := client.Download(ctx, id)
						require.NoError(b, err)
						downloaded.Free()
					}()
				}
				wg.Wait()
			}

			bytesProcessed := int64(b.N) * int64(bm.concurrency) * int64(bm.blobSize)
			b.ReportMetric(float64(bytesProcessed)/b.Elapsed().Seconds()/(1024*1024), "MiB/s")
		})
	}
}
