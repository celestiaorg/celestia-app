package appconsts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// maxAssumedBlockTime is the slow-end block time MaxAgeNumBlocks is derived
// against. Observed mainnet block time is ~2.85s; 3s leaves headroom.
const maxAssumedBlockTime = 3 * time.Second

// TestEvidenceWindowWithinUnbonding guards the invariant that the block-based
// evidence window does not outlive the unbonding period.
//
// Evidence is expired only once both bounds are exceeded (celestia-core
// evidence/verify.go and cosmossdk.io/x/evidence keeper/infraction.go both AND
// the duration and block-height comparisons), so the effective window is the
// larger of MaxAgeDuration and MaxAgeNumBlocks * blockTime. x/staking Slash()
// requires the infraction to be at most one unbonding period old; if the block
// bound admits older evidence, matured unbonding delegations have already been
// paid out and the slash over-burns whoever is still bonded.
//
// MaxAgeDuration already equals UnbondingTime, so only the block bound can
// exceed it. At any block time up to maxAssumedBlockTime the block window must
// stay within UnbondingTime.
func TestEvidenceWindowWithinUnbonding(t *testing.T) {
	blockWindow := time.Duration(MaxAgeNumBlocks) * maxAssumedBlockTime

	assert.LessOrEqual(t, blockWindow, UnbondingTime,
		"MaxAgeNumBlocks (%d) at %v/block admits evidence for %v, which exceeds UnbondingTime (%v)",
		MaxAgeNumBlocks, maxAssumedBlockTime, blockWindow, UnbondingTime)
	assert.Equal(t, MaxAgeDuration, UnbondingTime,
		"MaxAgeDuration must mirror UnbondingTime")
}
