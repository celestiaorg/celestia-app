package fibre

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestServerRPCMetricsBoundedAttributes(t *testing.T) {
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(ctx)) })
	metrics, err := newServerMetrics(provider.Meter("test"), newOccupancy(0))
	require.NoError(t, err)

	for _, observe := range []func(context.Context) func(int64, error){
		metrics.observeUploadShard, metrics.observeDownloadShard,
	} {
		for size := range int64(100) {
			observe(ctx)(size, nil)
			observe(ctx)(size, errors.New("request failed"))
		}
	}

	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &data))
	checked := 0
	for _, scope := range data.ScopeMetrics {
		for _, metric := range scope.Metrics {
			switch metric.Name {
			case "fibre.server.upload_shard.duration", "fibre.server.download_shard.duration":
				checked++
				require.Equal(t, "s", metric.Unit)
				histogram, ok := metric.Data.(metricdata.Histogram[float64])
				require.True(t, ok)
				require.Len(t, histogram.DataPoints, 2)
				outcomes := make(map[bool]bool)
				for _, point := range histogram.DataPoints {
					require.Equal(t, 1, point.Attributes.Len())
					success, ok := point.Attributes.Value("success")
					require.True(t, ok)
					outcomes[success.AsBool()] = true
					require.Equal(t, uint64(100), point.Count)
					require.Equal(t, []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}, point.Bounds)
				}
				require.Len(t, outcomes, 2)
			case "fibre.server.upload_shard.in_flight", "fibre.server.download_shard.in_flight":
				checked++
				sum, ok := metric.Data.(metricdata.Sum[int64])
				require.True(t, ok)
				require.Len(t, sum.DataPoints, 1)
				require.Zero(t, sum.DataPoints[0].Value)
			}
		}
	}
	require.Equal(t, 4, checked)
}
