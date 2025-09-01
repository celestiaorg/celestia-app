package appconsts

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConsts(t *testing.T) {
	t.Run("TestUpgradeHeightDelay should be 3", func(t *testing.T) {
		require.Equal(t, int64(3), TestUpgradeHeightDelay)
	})
	t.Run("ArabicaUpgradeHeightDelay should be 1 day of 6 second blocks", func(t *testing.T) {
		require.Equal(t, int64(1*60*60*24/6), ArabicaUpgradeHeightDelay)
		require.Equal(t, int64(14_400), ArabicaUpgradeHeightDelay)
	})
	t.Run("MochaUpgradeHeightDelay should be 2 days of 6 second blocks", func(t *testing.T) {
		require.Equal(t, int64(2*60*60*24/6), MochaUpgradeHeightDelay)
		require.Equal(t, int64(28_800), MochaUpgradeHeightDelay)
	})
	t.Run("MainnetUpgradeHeightDelay should be 7 days of 6 second blocks", func(t *testing.T) {
		require.Equal(t, int64(7*60*60*24/6), MainnetUpgradeHeightDelay)
		require.Equal(t, int64(100_800), MainnetUpgradeHeightDelay)
	})
}
