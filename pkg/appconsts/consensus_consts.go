package appconsts

import "time"

const (
	// GoalBlockTime is the target time interval between blocks. Since the block
	// interval isn't enforced at consensus, the real block interval isn't
	// guaranteed to exactly match GoalBlockTime. GoalBlockTime is currently targeted
	// through static timeouts (i.e. TimeoutPropose, TimeoutCommit).
	GoalBlockTime = time.Second * 15

	// MaxAgeDuration is the maximum age of evidence that can be submitted for
	// slashing. See CIP-037.
	MaxAgeDuration = 337 * time.Hour // (14 days + 1 hour)

	// MaxAgeNumBlocks is the maximum number of blocks for which evidence can be
	// submitted for slashing. See CIP-037.
	MaxAgeNumBlocks = 242_640
)
