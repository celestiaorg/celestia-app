package appconsts

import "time"

const (
	Version uint64 = 5
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

	// TestUpgradeHeightDelay is the number of blocks that chain-id "test" waits
	// after a MsgTryUpgrade to activate the next version.
	TestUpgradeHeightDelay = int64(3)
	// ArabicaUpgradeHeightDelay is the number of blocks that Arabica waits
	// after a MsgTryUpgrade to activate the next version. Assuming a block
	// interval of 6 seconds, this is 1 day.
	ArabicaUpgradeHeightDelay = int64(14_400)
	// MochaUpgradeHeightDelay is the number of blocks that Mocha waits
	// after a MsgTryUpgrade to activate the next version. Assuming a block
	// interval of 6 seconds, this is 2 days.
	MochaUpgradeHeightDelay = int64(28_800)
	// MainnetUpgradeHeightDelay is the number of blocks that Mainnet waits
	// after a MsgTryUpgrade to activate the next version. Assuming a block
	// interval of 6 seconds, this is 7 days.
	MainnetUpgradeHeightDelay = int64(100_800)
	// Deprecated: Use MainnetUpgradeHeightDelay instead.
	UpgradeHeightDelay = MainnetUpgradeHeightDelay
)
