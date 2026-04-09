package appconsts

import "time"

const (
	// MaxAgeDuration is the maximum age of evidence that can be submitted for
	// slashing. See CIP-037.
	MaxAgeDuration = 337 * time.Hour // (14 days + 1 hour)

	// MaxAgeNumBlocks is the maximum number of blocks for which evidence can be
	// submitted for slashing. This preserves the same wall-clock evidence
	// window (~16.85 days) as the previous value of 242,640 blocks at 6s,
	// scaled for ~2.6s block times. See CIP-048.
	MaxAgeNumBlocks = 559_940
)
