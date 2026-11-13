package fibre

import (
	"context"
	"testing"

	fibregrpc "github.com/celestiaorg/celestia-app/v11/fibre/internal/grpc"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestGRPCMetrics(t *testing.T) {
	srv, err := fibregrpc.Listen("127.0.0.1:0", 0, 1, fibregrpc.AdmissionConfig{Budget: 1 << 20, MaxMessage: 1 << 20})
	require.NoError(t, err)
	t.Cleanup(func() { srv.Stop(context.Background()) })

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	require.NoError(t, registerGRPCMetrics(provider.Meter("test"), srv))

	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &collected))
	got := map[string]bool{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			got[m.Name] = true
		}
	}
	for _, name := range []string{
		"fibre.server.request.reserved_bytes",
		"fibre.server.request.admission.waiting",
		"fibre.server.connections.open",
		"fibre.server.connections.rejected",
	} {
		require.True(t, got[name], name)
	}
}
