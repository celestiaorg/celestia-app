package appconsts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// maxAssumedBlockTime is the average v11 block time MaxAgeNumBlocks is derived
// from: the 0.8s floor plus 0.1s for heights that run past it.
const maxAssumedBlockTime = 900 * time.Millisecond

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
