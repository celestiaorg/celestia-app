package appconsts

import "time"

const (
	// MaxAgeDuration is the maximum age of evidence that can be submitted for
	// slashing. See CIP-037.
	MaxAgeDuration = 337 * time.Hour // (14 days + 1 hour)

	// MaxAgeNumBlocks is the maximum number of blocks for which evidence can be
	// submitted for slashing. See CIP-048.
	MaxAgeNumBlocks = 485_280
)
