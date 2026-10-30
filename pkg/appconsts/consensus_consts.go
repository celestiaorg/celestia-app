package appconsts

import "time"

const (
	// MaxAgeDuration is the maximum age of evidence that can be submitted for
	// slashing. See CIP-037.
	MaxAgeDuration = 337 * time.Hour // (14 days + 1 hour)

	// MaxAgeNumBlocks is the maximum number of blocks for which evidence can be
	// submitted for slashing. Evidence is stale only once both MaxAgeDuration
	// and MaxAgeNumBlocks are exceeded. UnbondingTime / 0.9s (337h at a 0.9s
	// average v11 block), so the block bound outlives the unbonding period
	// only if blocks average more than 0.9s.
	MaxAgeNumBlocks = 1_348_000
)
