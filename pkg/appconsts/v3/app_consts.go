package v3

import "time"

const (
	Version              uint64 = 3
	NextVersion          uint64 = 4
	SquareSizeUpperBound int    = 128
	SubtreeRootThreshold int    = 64
	TxSizeCostPerByte    uint64 = 10
	GasPerBlobByte       uint32 = 8
	MaxTxSize            int    = 2097152 // 2 MiB in bytes
	TimeoutPropose              = time.Millisecond * 3500
	TimeoutCommit               = time.Millisecond * 4200
	// UpgradeHeightDelay is the number of blocks after a quorum has been
	// reached that the chain should upgrade to the new version. Assuming a block
	// interval of 12 seconds, this is 7 days.
	UpgradeHeightDelay = int64(7 * 24 * 60 * 60 / 6) // 7 days * 24 hours * 60 minutes * 60 seconds / 6 seconds per block = 100,800 blocks.
)
