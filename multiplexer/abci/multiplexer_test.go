package abci

import (
	"testing"

	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/cosmos/cosmos-sdk/telemetry"
	"github.com/stretchr/testify/require"
)

// TestDuplicateTelemetryRegistration reproduces
// https://github.com/celestiaorg/celestia-app/issues/6601
//
// In multiplexer mode, both the parent process and child process (embedded
// binary) read the same app.toml. When telemetry.enabled=true with
// prometheus-retention-time > 0, calling telemetry.New() registers a
// PrometheusSink on the global prometheus.DefaultRegisterer. If telemetry.New()
// is called a second time (e.g. during a version switch when
// enableGRPCAndAPIServers calls startTelemetry again), the second registration
// fails with "duplicate metrics collector registration attempted" because the
// PrometheusSink is already registered.
func TestDuplicateTelemetryRegistration(t *testing.T) {
	cfg := serverconfig.Config{
		Telemetry: telemetry.Config{
			Enabled:                 true,
			ServiceName:             "test",
			PrometheusRetentionTime: 60, // positive value enables Prometheus sink
		},
	}

	// First call to startTelemetry succeeds and registers a PrometheusSink on
	// the global prometheus.DefaultRegisterer.
	metrics, err := startTelemetry(cfg)
	require.NoError(t, err)
	require.NotNil(t, metrics)

	// Second call to startTelemetry fails because the PrometheusSink is
	// already registered on the global prometheus.DefaultRegisterer.
	_, err = startTelemetry(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate metrics collector registration attempted")
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
