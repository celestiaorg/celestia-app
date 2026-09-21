//go:build !benchmarks

package appconsts

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPayForFibreLimitActivation(t *testing.T) {
	for _, version := range []uint64{0, 9, 10} {
		require.Equal(t, 200, GetMaxPayForFibreMessages(version))
	}
	require.Equal(t, 2_000, GetMaxPayForFibreMessages(11))
	require.Equal(t, 2_000, GetMaxPayForFibreMessages(12))
	require.Equal(t, int64(33_231), GetUpgradeHeightDelay(CortoChainID))
}
