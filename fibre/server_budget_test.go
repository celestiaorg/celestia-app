package fibre

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeriveBudget(t *testing.T) {
	require.Equal(t, int64(math.MaxInt64), deriveBudget(math.MaxInt64, 4096, 4096))

	// Stake-weighted split.
	require.Equal(t, int64(25), deriveBudget(100, 1, 4))
	require.Equal(t, int64(50), deriveBudget(100, 2, 4))

	// Non-positive inputs disable the limiter instead of dividing by zero.
	require.Zero(t, deriveBudget(0, 10, 100))
	require.Zero(t, deriveBudget(100, 0, 100))
	require.Zero(t, deriveBudget(100, 10, 0))
}
