package fibre

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestClientMetricsObserveDial(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	m, err := newClientMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)

	m.observeDial("plaintext", false)
	m.observeDial("tls", true) // a plaintext->TLS fallback
	m.observeDial("tls", false)

	require.Equal(t, int64(1), sumCounter(t, reader, "fibre.client.dials", "plaintext"))
	require.Equal(t, int64(2), sumCounter(t, reader, "fibre.client.dials", "tls"))
	require.Equal(t, int64(1), sumCounter(t, reader, "fibre.client.dial.fallbacks", ""))
}

func TestServerMetricsObserveAccept(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	m, err := newServerMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"), newOccupancy(0))
	require.NoError(t, err)

	m.observeAccept("plaintext")
	m.observeAccept("plaintext")
	m.observeAccept("tls")

	require.Equal(t, int64(2), sumCounter(t, reader, "fibre.server.accepts", "plaintext"))
	require.Equal(t, int64(1), sumCounter(t, reader, "fibre.server.accepts", "tls"))
}

// sumCounter returns the value of the named Int64 sum counter, restricted to
// the given mode attribute when mode is non-empty.
func sumCounter(t *testing.T, reader sdkmetric.Reader, name, mode string) int64 {
	t.Helper()
	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &data))
	for _, scope := range data.ScopeMetrics {
		for _, mm := range scope.Metrics {
			if mm.Name != name {
				continue
			}
			var total int64
			for _, pt := range mm.Data.(metricdata.Sum[int64]).DataPoints {
				if mode == "" {
					total += pt.Value
					continue
				}
				if v, ok := pt.Attributes.Value(attribute.Key("mode")); ok && v.AsString() == mode {
					total += pt.Value
				}
			}
			return total
		}
	}
	t.Fatalf("metric %q not found", name)
	return 0
}
