package cmd

import (
	"testing"

	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/cosmos/cosmos-sdk/telemetry"
	"github.com/stretchr/testify/require"
)

func TestStartTelemetry(t *testing.T) {
	cfg := serverconfig.Config{
		Telemetry: telemetry.Config{
			Enabled:                 true,
			ServiceName:             "test",
			PrometheusRetentionTime: 60,
		},
	}

	// First call should succeed
	metrics, err := startTelemetry(cfg)
	require.NoError(t, err)
	require.NotNil(t, metrics)

	// Second call should fail with duplicate registration
	_, err = startTelemetry(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate metrics collector registration attempted")
}
