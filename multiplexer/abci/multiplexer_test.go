package abci

import (
	"testing"

	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/cosmos/cosmos-sdk/telemetry"
	"github.com/stretchr/testify/require"
)

// TestInitTelemetryIsIdempotent verifies the fix for
// https://github.com/celestiaorg/celestia-app/issues/6601
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

func TestRemoveStart(t *testing.T) {
	type testCase struct {
		name  string
		input []string
		want  []string
	}
	tests := []testCase{
		{
			name:  "should return empty slice if no args are provided",
			input: []string{},
			want:  []string{},
		},
		{
			name:  "should return empty slice if just celestia-appd is provided",
			input: []string{"celestia-appd"},
			want:  []string{},
		},
		{
			name:  "should remove celestia-appd and start from input",
			input: []string{"celestia-appd", "start", "--home", "foo"},
			want:  []string{"--home", "foo"},
		},
		{
			name:  "should preserve extra additional args",
			input: []string{"celestia-appd", "start", "--home", "foo", "--grpc.enable", "--api.enable"},
			want:  []string{"--home", "foo", "--grpc.enable", "--api.enable"},
		},
		{
			// Reproduces https://github.com/celestiaorg/celestia-app/issues/4926
			name:  "should preserve --home if included before start",
			input: []string{"celestia-appd", "--home", "foo", "start"},
			want:  []string{"--home", "foo"},
		},
	}
	for _, test := range tests {
		got := removeStart(test.input)
		require.Equal(t, test.want, got)
	}
}
