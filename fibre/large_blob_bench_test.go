package fibre_test

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
)

// TestLargeBlobProtocolProfiles exercises the cheap configuration path for the
// proposed 10x and 20x profiles. Full encoding is deliberately a benchmark:
// its expected working sets are roughly 15 GiB and 30 GiB respectively.
func TestLargeBlobProtocolProfiles(t *testing.T) {
	for _, factor := range []int{10, 20} {
		t.Run(fmt.Sprintf("%dx", factor), func(t *testing.T) {
			maxBlobSize := factor * fibre.DefaultProtocolParams.MaxBlobSize

			clientCfg := fibre.DefaultClientConfig()
			require.NoError(t, clientCfg.SetMaxBlobSize(maxBlobSize))

			serverCfg := fibre.DefaultServerConfig()
			require.NoError(t, serverCfg.SetMaxBlobSize(maxBlobSize))

			require.Equal(t, maxBlobSize-5, clientCfg.BlobConfig.MaxDataSize)
			require.Equal(t, factor*32<<10, clientCfg.BlobConfig.MaxRowSize)
			require.Equal(t, clientCfg.BlobConfig.MaxRowSize, serverCfg.BlobConfig.MaxRowSize)
			require.Equal(t, clientCfg.MaxMessageSize, serverCfg.MaxMessageSize)
			require.LessOrEqual(t, uint64(clientCfg.MaxMessageSize), uint64(^uint32(0)))
		})
	}
}

// BenchmarkLargeBlobEncode measures the dominant client-side cost. By default
// it runs through 4x so it is safe on common 16 GiB developer machines. A
// high-memory benchmark host can opt into the target profiles with, for example:
//
//	FIBRE_LARGE_BLOB_BENCH_FACTORS=1,10,20 go test ./fibre \
//	  -run=^$ -bench=BenchmarkLargeBlobEncode -benchtime=1x -benchmem -timeout=2h
func BenchmarkLargeBlobEncode(b *testing.B) {
	for _, factor := range largeBlobBenchFactors(b) {
		b.Run(fmt.Sprintf("%dx_%d_MiB", factor, factor*128), func(b *testing.B) {
			params := fibre.DefaultProtocolParams
			params.MaxBlobSize *= factor
			cfg, err := fibre.NewBlobConfigFromParams(0, params)
			require.NoError(b, err)

			data := make([]byte, cfg.MaxDataSize)
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				blob, err := fibre.NewBlob(data, cfg)
				require.NoError(b, err)
				blob.Free()
			}
			b.ReportMetric(float64(12*params.MaxBlobSize)/(1<<30), "approx-peak-GiB")
			runtime.KeepAlive(data)
		})
	}
}

// BenchmarkLargeBlobEqualStakeShardStore models one of 100 equal-stake
// validators. The 100-bit unique-decoding floor assigns 148 rows, so this
// exercises realistic 10x/20x per-validator shard sizes without allocating the
// encoder's 12x working set.
func BenchmarkLargeBlobEqualStakeShardStore(b *testing.B) {
	for _, factor := range []int{1, 10, 20} {
		b.Run(fmt.Sprintf("%dx_%d_MiB_blob", factor, factor*128), func(b *testing.B) {
			params := fibre.DefaultProtocolParams
			params.MaxBlobSize *= factor
			rowSize := params.MaxRowSize(0)
			rowCount := params.MinRowsPerValidator()

			proof := make([][]byte, 14)
			for i := range proof {
				proof[i] = make([]byte, 32)
			}
			shard := &types.BlobShard{
				Rows: make([]*types.BlobRow, rowCount),
				Rlcs: make([]byte, params.Rows*16),
			}
			for i := range shard.Rows {
				shard.Rows[i] = &types.BlobRow{
					Index: uint32(i),
					Data:  make([]byte, rowSize),
					Proof: proof,
				}
			}

			blob := makeBenchBlob(factor)
			defer blob.Free()
			promise := makeTestPaymentPromise(uint64(factor), blob.ID())
			store := makeBenchStore(b)
			defer store.Close()

			shardBytes := int64(rowCount*rowSize + rowCount*14*32 + len(shard.Rlcs))
			b.SetBytes(shardBytes)
			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				err := store.Put(b.Context(), promise, shard, time.Now().Add(time.Hour))
				require.NoError(b, err)
			}
			b.ReportMetric(float64(shardBytes)/(1<<20), "shard-MiB")
		})
	}
}

func largeBlobBenchFactors(tb testing.TB) []int {
	value := os.Getenv("FIBRE_LARGE_BLOB_BENCH_FACTORS")
	if value == "" {
		return []int{1, 2, 4}
	}

	parts := strings.Split(value, ",")
	factors := make([]int, 0, len(parts))
	for _, part := range parts {
		factor, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || factor <= 0 {
			tb.Fatalf("invalid FIBRE_LARGE_BLOB_BENCH_FACTORS value %q", part)
		}
		factors = append(factors, factor)
	}
	return factors
}
