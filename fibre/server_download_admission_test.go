package fibre

import (
	"context"
	"log/slog"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	fibregrpc "github.com/celestiaorg/celestia-app/v10/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	pebbledb "github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type downloadBudgetBackend struct {
	shardBackend
	reader   *sdkmetric.ManualReader
	reads    atomic.Int32
	reserved atomic.Int64
}

func (b *downloadBudgetBackend) Get(ctx context.Context, commitment Commitment, hash []byte) (*types.BlobShard, error) {
	if hash[0] == 2 {
		b.reads.Add(1)
		var data metricdata.ResourceMetrics
		if err := b.reader.Collect(ctx, &data); err != nil {
			return nil, err
		}
		for _, scope := range data.ScopeMetrics {
			for _, metric := range scope.Metrics {
				if metric.Name == "fibre.server.rpc.reserved_bytes" {
					b.reserved.Store(metric.Data.(metricdata.Gauge[int64]).DataPoints[0].Value)
				}
			}
		}
	}
	return b.shardBackend.Get(ctx, commitment, hash)
}

func TestDownloadReservesFallbackBeforeRead(t *testing.T) {
	for _, budget := range []int64{8 << 20, 128 << 20} {
		t.Run(strconv.FormatInt(budget, 10), func(t *testing.T) {
			store := newMarkerTestStore(t)
			commitment := generateCommitment()
			large := &types.BlobShard{Rows: make([]*types.BlobRow, 256)}
			for i := range large.Rows {
				large.Rows[i] = &types.BlobRow{Index: uint32(i), Data: make([]byte, 4096)}
			}
			for i, shard := range []*types.BlobShard{{Rows: large.Rows[:1]}, large} {
				hash := []byte{byte(i + 1)}
				marker := store.shards.marker(shardBinarySize(shard))
				require.NoError(t, store.shards.Put(t.Context(), marker, commitment, hash, shard))
				require.NoError(t, store.db.Set(shardKey(commitment, hash), marker, pebbledb.NoSync))
			}
			require.NoError(t, store.shards.primary.Delete(t.Context(), commitment, []byte{1}))
			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
			meter := provider.Meter("download-admission-test")
			backend := &downloadBudgetBackend{shardBackend: store.shards.primary, reader: reader}
			store.shards.primary = backend
			metrics, err := newServerMetrics(meter, newOccupancy(0))
			require.NoError(t, err)
			service := &Server{
				Config: DefaultServerConfig(), store: store, metrics: metrics,
				log: slog.Default(), tracer: noop.NewTracerProvider().Tracer("test"),
			}
			admission := fibregrpc.NewAdmission(budget, service.Config.MaxMessageSize, 4096, 14)
			admission.DownloadSize = service.estimateDownloadSize
			require.NoError(t, admission.RegisterMetrics(meter))
			server, err := fibregrpc.Listen("127.0.0.1:0", 1, 1)
			require.NoError(t, err)
			server.Register(service, admission, nil, nil)
			server.Serve()
			t.Cleanup(func() { server.Stop(context.Background()) })
			conn, err := grpc.NewClient(server.ListenAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithDefaultCallOptions(grpc.CallContentSubtype("fibre-proto")))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, conn.Close()) })
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			response, err := types.NewFibreClient(conn).DownloadShard(ctx, &types.DownloadShardRequest{BlobId: NewBlobID(0, commitment)})
			if budget == 8<<20 {
				require.Equal(t, codes.ResourceExhausted, status.Code(err))
				require.Zero(t, backend.reads.Load(), "reject the larger copy before its backend read")
				return
			}
			require.NoError(t, err)
			require.Len(t, response.Shard.Rows, len(large.Rows))
			require.EqualValues(t, 1, backend.reads.Load())
			want := 6*shardBinarySize(large) + 4096*(256+64*14) + 1<<20
			require.Equal(t, want, backend.reserved.Load(), "reserve the selected copy before its backend read")
		})
	}
}
