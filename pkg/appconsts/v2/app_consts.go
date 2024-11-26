package v2

import "time"

const (
	Version              uint64 = 2
	SquareSizeUpperBound int    = 128
	SubtreeRootThreshold int    = 64
	// TimeoutPropose wasn't a constant in v2, it was the default for a
	// user-configurable timeout.
	TimeoutPropose = time.Second * 10
	// TimeoutCommit wasn't a constant in v2, it was the default for a
	// user-configurable timeout.
	TimeoutCommit = time.Second * 11
	// UpgradeHeightDelay is the number of blocks after a quorum has been
	// reached that the chain should upgrade to the new version. Assuming a block
	// interval of 12 seconds, this is 7 days.
	UpgradeHeightDelay = int64(7 * 24 * 60 * 60 / 12) // 7 days * 24 hours * 60 minutes * 60 seconds / 12 seconds per block = 50,400 blocks.
)
