package appconsts

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMaxExpectedTimePerBlock fails if MaxExpectedTimePerBlock is not 5x the
// v10 block interval.
func TestMaxExpectedTimePerBlock(t *testing.T) {
	want := 5 * GetConsensusTimeouts(10).BlockInterval()
	assert.Equal(t, want, MaxExpectedTimePerBlock,
		"If you changed timeout constants, also update MaxExpectedTimePerBlock.")
}

// TestSubtreeRootThreshold verifies that SubtreeRootThreshold has not changed
// from 64. SubtreeRootThreshold is hard-coded in clients (e.g. Lumina) and
// changing it is a breaking change that requires cross-team coordination. If
// this test fails, you likely need to revert the change to SubtreeRootThreshold.
// See https://github.com/celestiaorg/celestia-app/issues/6831
func TestSubtreeRootThreshold(t *testing.T) {
	require.Equal(t, 64, SubtreeRootThreshold)
}

func TestConsts(t *testing.T) {
	t.Run("TestUpgradeHeightDelay should be 3", func(t *testing.T) {
		require.Equal(t, int64(3), TestUpgradeHeightDelay)
	})
	t.Run("CortoUpgradeHeightDelay should be 1 day of ~2.6 second blocks", func(t *testing.T) {
		require.Equal(t, int64(33_231), CortoUpgradeHeightDelay)
	})
	t.Run("MochaUpgradeHeightDelay should be 2 days of ~2.6 second blocks", func(t *testing.T) {
		require.Equal(t, int64(66_462), MochaUpgradeHeightDelay)
	})
	t.Run("MainnetUpgradeHeightDelay should be 7 days of ~2.6 second blocks", func(t *testing.T) {
		require.Equal(t, int64(232_616), MainnetUpgradeHeightDelay)
	})
}
