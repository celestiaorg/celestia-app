package v1

import "time"

const (
	Version              uint64 = 1
	SquareSizeUpperBound int    = 128
	SubtreeRootThreshold int    = 64
	// TimeoutPropose is deprecated because it was not a constant
	// in v1, it was the default for a user-configurable timeout.
	TimeoutPropose = time.Second * 10
	// TimeoutCommit is deprecated because it was not a constant
	// in v1, it was the default for a user-configurable timeout.
	TimeoutCommit = time.Second * 11
	// UpgradeHeightDelay is deprecated because v1 does not contain the signal
	// module so this constant should not be used.
	UpgradeHeightDelay = int64(7 * 24 * 60 * 60 / 12) // 7 days * 24 hours * 60 minutes * 60 seconds / 12 seconds per block = 50,400 blocks.
)
