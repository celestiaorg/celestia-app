package appconsts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMaxExpectedTimePerBlock fails if MaxExpectedTimePerBlock is not 5x the
// block interval of the current app version.
func TestMaxExpectedTimePerBlock(t *testing.T) {
	want := 5 * GetConsensusTimeouts(Version).BlockInterval()
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
	blocksIn := func(d time.Duration) int64 { return int64(d / GetConsensusTimeouts(Version).BlockInterval()) }
	day := 24 * time.Hour
	t.Run("CortoUpgradeHeightDelay should be 1 day of 0.8 second blocks", func(t *testing.T) {
		require.Equal(t, blocksIn(day), CortoUpgradeHeightDelay)
	})
	t.Run("MochaUpgradeHeightDelay should be 2 days of 0.8 second blocks", func(t *testing.T) {
		require.Equal(t, blocksIn(2*day), MochaUpgradeHeightDelay)
	})
	t.Run("MainnetUpgradeHeightDelay should be 7 days of 0.8 second blocks", func(t *testing.T) {
		require.Equal(t, blocksIn(7*day), MainnetUpgradeHeightDelay)
	})
}
