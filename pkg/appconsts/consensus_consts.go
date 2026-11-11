package appconsts

import "time"

const (
	// MaxAgeDuration is the maximum age of evidence that can be submitted for
	// slashing. See CIP-037.
	MaxAgeDuration = 337 * time.Hour // (14 days + 1 hour)

	// MaxAgeNumBlocks is the maximum number of blocks for which evidence can be
	// submitted for slashing. Evidence is stale only once both MaxAgeDuration
	// and MaxAgeNumBlocks are exceeded. UnbondingTime / 1.2s: blocks averaged
	// 0.85-1.2s on the geo100 run, so the block bound outlives the unbonding
	// period only if they average more than 1.2s.
	MaxAgeNumBlocks = 1_011_000
)
