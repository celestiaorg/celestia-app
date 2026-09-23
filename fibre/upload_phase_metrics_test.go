package fibre

import (
	"context"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"testing"
	"time"
)

func TestUploadPhaseBalancesCanceledContext(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	m, err := newServerMetrics(provider.Meter("test"), newOccupancy(0))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	done := m.phase(ctx, "packed_admission_wait", 77)
	cancel()
	done()
	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &data))
	seen := 0
	for _, s := range data.ScopeMetrics {
		for _, v := range s.Metrics {
			switch v.Name {
			case "fibre.server.upload.phase.active", "fibre.server.upload.phase.bytes":
				for _, p := range v.Data.(metricdata.Sum[int64]).DataPoints {
					require.Zero(t, p.Value)
				}
				seen++
			case "fibre.server.upload.phase.duration":
				points := v.Data.(metricdata.Histogram[float64]).DataPoints
				require.Len(t, points, 1)
				require.Equal(t, uint64(1), points[0].Count)
				require.Equal(t, float64(300), points[0].Bounds[len(points[0].Bounds)-1])
				seen++
			}
		}
	}
	require.Equal(t, 3, seen)
}

func TestPackedQueueMetricsBalanceOnCancellation(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	m, err := newServerMetrics(provider.Meter("test"), newOccupancy(0))
	require.NoError(t, err)
	b, _ := newPackedTestBackend(t, 16)
	b.object.metrics = m
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() { result <- b.Put(ctx, Commitment{}, []byte{1}, &types.BlobShard{Rlcs: []byte("data")}) }()
	require.Eventually(t, func() bool { b.mu.Lock(); defer b.mu.Unlock(); return len(b.queues[1].requests) == 1 }, time.Second, time.Millisecond)
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &data))
	for _, s := range data.ScopeMetrics {
		for _, v := range s.Metrics {
			if v.Name == "fibre.server.upload.phase.active" || v.Name == "fibre.server.upload.phase.bytes" {
				for _, p := range v.Data.(metricdata.Sum[int64]).DataPoints {
					require.Zero(t, p.Value, p.Attributes)
				}
			}
		}
	}
}
