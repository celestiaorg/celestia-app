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

func TestMemoryAdmission(t *testing.T) {
	a := newMemoryBudget(100)
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	require.NoError(t, a.RegisterMetrics(provider.Meter("admission-test")))
	download, upload := newMemoryLease(a, true), newMemoryLease(a, false)
	require.NoError(t, download.reserve(t.Context(), 25))
	require.NoError(t, upload.reserve(t.Context(), 50))
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
	require.EqualValues(t, 75, values["fibre.server.rpc.reserved_bytes"])
	require.EqualValues(t, 50, values["fibre.server.rpc.upload_reserved_bytes"])
	require.Equal(t, codes.ResourceExhausted, status.Code(upload.reserve(t.Context(), 1)))
	require.NoError(t, download.reserve(t.Context(), 25), "downloads can fill the remaining budget")
	require.Equal(t, codes.ResourceExhausted, status.Code(download.reserve(t.Context(), 1)))
	download.release()
	require.NoError(t, upload.reserve(t.Context(), 25), "uploads can use three quarters of the budget")
	require.Equal(t, codes.ResourceExhausted, status.Code(upload.reserve(t.Context(), 1)))
	upload.release()
	require.Zero(t, a.used)
	require.Zero(t, a.downloads)
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			l := newMemoryLease(a, true)
			_ = l.reserve(t.Context(), 10)
			l.release()
		})
	}
	wg.Wait()
	require.Zero(t, a.used)
	disabled := newMemoryBudget(0)
	l := newMemoryLease(disabled, true)
	require.NoError(t, l.reserve(t.Context(), 1000))
	require.EqualValues(t, 1000, disabled.used)
	require.EqualValues(t, 1000, disabled.downloads)
	l.release()
	require.Zero(t, disabled.used)
	require.Zero(t, disabled.downloads)
}

func TestMemoryResponseLifetime(t *testing.T) {
	a := newMemoryBudget(100)
	l := newMemoryLease(a, true)
	require.NoError(t, l.reserve(t.Context(), 100))
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
