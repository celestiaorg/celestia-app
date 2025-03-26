package v4

import "time"

const (
	Version uint64 = 4
	// SquareSizeUpperBound imposes an upper bound on the max effective square size.
	SquareSizeUpperBound int = 128
	// SubtreeRootThreshold works as a target upper bound for the number of subtree
	// roots in the share commitment. If a blob contains more shares than this
	// number, then the height of the subtree roots will increase by one so that the
	// number of subtree roots in the share commitment decreases by a factor of two.
	// This step is repeated until the number of subtree roots is less than the
	// SubtreeRootThreshold.
	//
	// The rationale for this value is described in more detail in ADR-013.
	SubtreeRootThreshold int    = 64
	TxSizeCostPerByte    uint64 = 10
	GasPerBlobByte       uint32 = 8
	MaxTxSize            int    = 2097152 // 2 MiB in bytes
	TimeoutPropose              = time.Millisecond * 3500
	TimeoutCommit               = time.Millisecond * 4200
	// UpgradeHeightDelay is the number of blocks after a quorum has been
	// reached that the chain should upgrade to the new version. Assuming a block
	// interval of 6 seconds, this is 7 days.
	UpgradeHeightDelay = int64(7 * 24 * 60 * 60 / 6) // 7 days * 24 hours * 60 minutes * 60 seconds / 6 seconds per block = 100,800 blocks.
)
