package appconsts

import "time"

const (
	// MaxAgeDuration is the maximum age of evidence that can be submitted for
	// slashing. See CIP-037.
	MaxAgeDuration = 337 * time.Hour // (14 days + 1 hour)

	// MaxAgeNumBlocks is the maximum number of blocks for which evidence can be
	// submitted for slashing. Evidence is stale only once both MaxAgeDuration
	// and MaxAgeNumBlocks are exceeded. This is the v10 value (UnbondingTime /
	// 3s = 404,400) x3.25 for 0.8s blocks, so the block bound outlives the
	// unbonding period once blocks average more than 0.923s.
	MaxAgeNumBlocks = 1_314_300
)
