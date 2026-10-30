package appconsts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// maxAssumedBlockTime is the slow-end block time MaxAgeNumBlocks stays within
// the unbonding period at. The v11 0.8s floor leaves 0.123s of headroom, far
// less than the 3s bound #7706 chose against ~2.85s observed v10 blocks.
const maxAssumedBlockTime = 923 * time.Millisecond

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
