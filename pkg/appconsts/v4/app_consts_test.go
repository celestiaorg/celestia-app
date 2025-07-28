package v4

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConsts(t *testing.T) {
	t.Run("TestUpgradeHeightDelay should be 3", func(t *testing.T) {
		require.Equal(t, int64(3), TestUpgradeHeightDelay)
	})
	t.Run("ArabicaUpgradeHeightDelay should be 1 block", func(t *testing.T) {
		require.Equal(t, int64(1), ArabicaUpgradeHeightDelay)
	})
	t.Run("MochaUpgradeHeightDelay should be 1 block", func(t *testing.T) {
		require.Equal(t, int64(1), MochaUpgradeHeightDelay)
	})
	t.Run("MainnetUpgradeHeightDelay should be 1 block", func(t *testing.T) {
		require.Equal(t, int64(1), MainnetUpgradeHeightDelay)
	})
}
