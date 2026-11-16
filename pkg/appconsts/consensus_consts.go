package appconsts

import "time"

const (
	// MaxAgeDuration is the maximum age of evidence that can be submitted for
	// slashing. See CIP-037.
	MaxAgeDuration = 337 * time.Hour // (14 days + 1 hour)

	// MaxAgeNumBlocks is the maximum number of blocks for which evidence can be
	// submitted for slashing. Evidence is stale only once both MaxAgeDuration
	// and MaxAgeNumBlocks are exceeded. UnbondingTime / 1.2s (to give some
	// headroom for congestion).
	MaxAgeNumBlocks = 1_011_000
)
