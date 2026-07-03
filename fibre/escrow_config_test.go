package fibre

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
)

func TestEscrowConfigValidateFillsDefaults(t *testing.T) {
	var e EscrowConfig // zero value: nil watermarks, zero intervals
	require.NoError(t, e.Validate())

	d := defaultEscrowConfig(DefaultProtocolParams)
	require.True(t, e.LowWatermark.Equal(d.LowWatermark))
	require.True(t, e.HighWatermark.Equal(d.HighWatermark))
	require.Equal(t, d.RefillCheckInterval, e.RefillCheckInterval)
}

func TestEscrowConfigValidateRejectsBadWatermarks(t *testing.T) {
	e := defaultEscrowConfig(DefaultProtocolParams)
	e.HighWatermark = e.LowWatermark // not strictly greater
	require.Error(t, e.Validate())

	e = defaultEscrowConfig(DefaultProtocolParams)
	e.LowWatermark = math.ZeroInt()
	require.Error(t, e.Validate())
}

func TestDefaultEscrowConfigWatermarksOrdered(t *testing.T) {
	d := defaultEscrowConfig(DefaultProtocolParams)
	require.True(t, d.AutoFund)
	require.True(t, d.LowWatermark.IsPositive())
	require.True(t, d.HighWatermark.GT(d.LowWatermark))
	require.Positive(t, d.RefillCheckInterval)
}
