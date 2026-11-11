package appconsts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// maxAssumedBlockTime is the upper bound of the measured v11 average block
// times (0.85-1.2s on the geo100 run) that MaxAgeNumBlocks is derived from.
const maxAssumedBlockTime = 1200 * time.Millisecond

// TestEvidenceWindowWithinUnbonding guards that the block-based evidence window
// does not outlive the unbonding period. Evidence expires only once both bounds
// are exceeded, so the effective window is the larger of MaxAgeDuration and
// MaxAgeNumBlocks * blockTime; admitting evidence older than the unbonding
// period lets the slash over-burn delegators who are still bonded.
func TestEvidenceWindowWithinUnbonding(t *testing.T) {
	blockWindow := time.Duration(MaxAgeNumBlocks) * maxAssumedBlockTime

	assert.LessOrEqual(t, blockWindow, UnbondingTime,
		"MaxAgeNumBlocks (%d) at %v/block admits evidence for %v, which exceeds UnbondingTime (%v)",
		MaxAgeNumBlocks, maxAssumedBlockTime, blockWindow, UnbondingTime)
	assert.Equal(t, MaxAgeDuration, UnbondingTime,
		"MaxAgeDuration must mirror UnbondingTime")
}
