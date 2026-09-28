package abci

import (
	"testing"

	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/cosmos/cosmos-sdk/telemetry"
	"github.com/stretchr/testify/require"
)

// TestInitTelemetryIsIdempotent verifies that initTelemetry is idempotent.
//
// When telemetry.enabled=true with prometheus-retention-time > 0,
// telemetry.New() registers a PrometheusSink on the global
// prometheus.DefaultRegisterer. In multiplexer mode,
// enableGRPCAndAPIServers may be called more than once (e.g. during a
// version switch). Without caching, the second telemetry.New() call fails
// with "duplicate metrics collector registration attempted".
//
// initTelemetry must be idempotent: the second call should be a no-op.
// Removing the caching guard causes this test to fail.
func TestInitTelemetryIsIdempotent(t *testing.T) {
	m := &Multiplexer{
		svrCfg: serverconfig.Config{
			Telemetry: telemetry.Config{
				Enabled:                 true,
				ServiceName:             "test",
				PrometheusRetentionTime: 60, // positive value enables Prometheus sink
			},
		},
	}

	// First call registers a PrometheusSink on the global
	// prometheus.DefaultRegisterer.
	require.NoError(t, m.initTelemetry())
	require.NotNil(t, m.metrics)

	// Second call must succeed. Without the caching guard this fails with
	// "duplicate metrics collector registration attempted".
	require.NoError(t, m.initTelemetry())
}

func TestOpenTraceWriter(t *testing.T) {
	t.Run("openTraceWriter with empty file does not error", func(t *testing.T) {
		_, err := openTraceWriter("")
		require.NoError(t, err)
	})
}
