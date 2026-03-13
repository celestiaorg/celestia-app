package appconsts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMaxExpectedTimePerBlock fails if MaxExpectedTimePerBlock deviates too
// much from the expected block time * 5. The expected block time is primarily
// determined by DelayedPrecommitTimeout. If this test fails, it means that
// timeout constants were modified without updating MaxExpectedTimePerBlock (or
// vice versa). All of these values need to be updated together:
//   - DelayedPrecommitTimeout (and other Timeout* constants)
//   - MaxExpectedTimePerBlock
func TestMaxExpectedTimePerBlock(t *testing.T) {
	expectedBlockTime := DelayedPrecommitTimeout
	want := expectedBlockTime * 5
	deviation := MaxExpectedTimePerBlock - want
	if deviation < 0 {
		deviation = -deviation
	}
	// Allow up to 2 seconds of tolerance to account for the fact that
	// DelayedPrecommitTimeout (2790ms) isn't exactly 3 seconds.
	tolerance := 2 * time.Second
	assert.LessOrEqual(t, deviation, tolerance,
		"MaxExpectedTimePerBlock (%v) deviates from DelayedPrecommitTimeout * 5 (%v) by more than %v. "+
			"If you changed timeout constants, also update MaxExpectedTimePerBlock.",
		MaxExpectedTimePerBlock, want, tolerance)
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
	t.Run("ArabicaUpgradeHeightDelay should be 1 day of 3 second blocks", func(t *testing.T) {
		require.Equal(t, int64(1*60*60*24/3), ArabicaUpgradeHeightDelay)
		require.Equal(t, int64(28_800), ArabicaUpgradeHeightDelay)
	})
	t.Run("MochaUpgradeHeightDelay should be 2 days of 3 second blocks", func(t *testing.T) {
		require.Equal(t, int64(2*60*60*24/3), MochaUpgradeHeightDelay)
		require.Equal(t, int64(57_600), MochaUpgradeHeightDelay)
	})
	t.Run("MainnetUpgradeHeightDelay should be 7 days of 3 second blocks", func(t *testing.T) {
		require.Equal(t, int64(7*60*60*24/3), MainnetUpgradeHeightDelay)
		require.Equal(t, int64(201_600), MainnetUpgradeHeightDelay)
	})
}
