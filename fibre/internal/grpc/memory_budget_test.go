package grpc

import (
	"context"
	"sync"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func testLease(a *memoryBudget, download bool) *memoryLease {
	l := &memoryLease{budget: a, download: download}
	l.refs.Store(1)
	return l
}

func TestMemoryAdmission(t *testing.T) {
	a := newMemoryBudget(100, 60)
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	require.NoError(t, a.RegisterMetrics(provider.Meter("admission-test")))
	download, upload := testLease(a, true), testLease(a, false)
	require.NoError(t, download.reserve(40))
	require.Equal(t, codes.ResourceExhausted, status.Code(download.reserve(1)))
	require.NoError(t, upload.reserve(60))
	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &metrics))
	values := make(map[string]int64)
	for _, scope := range metrics.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if gauge, ok := metric.Data.(metricdata.Gauge[int64]); ok {
				values[metric.Name] = gauge.DataPoints[0].Value
			}
		}
	}
	require.EqualValues(t, 100, values["fibre.server.rpc.reserved_bytes"])
	require.EqualValues(t, 60, values["fibre.server.rpc.upload_reserved_bytes"])
	require.Equal(t, codes.ResourceExhausted, status.Code(upload.reserve(1)))
	download.release()
	require.NoError(t, upload.reserve(40), "uploads can use the entire budget")
	upload.release()
	require.Zero(t, a.used)
	require.Zero(t, a.downloads)
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			l := testLease(a, true)
			_ = l.reserve(10)
			l.release()
		})
	}
	wg.Wait()
	require.Zero(t, a.used)
	disabled := newMemoryBudget(0, 60)
	l := testLease(disabled, true)
	require.NoError(t, l.reserve(1000))
	l.release()
}

func TestMemoryResponseLifetime(t *testing.T) {
	a := newMemoryBudget(100, 0)
	l := testLease(a, true)
	require.NoError(t, l.reserve(100))
	codec := &pooledCodec{pool: l}
	data, err := codec.Marshal(&types.UploadShardResponse{ValidatorSignature: []byte{1}})
	require.NoError(t, err)
	data.Ref()
	l.release()
	data.Free()
	require.EqualValues(t, 100, a.used)
	data.Free()
	require.Zero(t, a.used)
}
