package appconsts

import (
	"time"

	"cosmossdk.io/math"
)

const (
	// Version is the current application version.
	Version uint64 = 6
	// SquareSizeUpperBound imposes an upper bound on the max effective square size.
	SquareSizeUpperBound int = 512
	// SubtreeRootThreshold works as a target upper bound for the number of subtree
	// roots in the share commitment. If a blob contains more shares than this
	// number, then the height of the subtree roots will increase by one so that the
	// number of subtree roots in the share commitment decreases by a factor of two.
	// This step is repeated until the number of subtree roots is less than the
	// SubtreeRootThreshold.
	//
	// The rationale for this value is described in more detail in ADR-013.
	SubtreeRootThreshold    int    = 64
	TxSizeCostPerByte       uint64 = 10
	GasPerBlobByte          uint32 = 8
	MaxTxSize               int    = 8_388_608 // 8 MiB in bytes
	TimeoutPropose                 = time.Millisecond * 8500
	TimeoutProposeDelta            = time.Millisecond * 500
	TimeoutPrevote                 = time.Millisecond * 3000
	TimeoutPrevoteDelta            = time.Millisecond * 500
	TimeoutPrecommit               = time.Millisecond * 3000
	TimeoutPrecommitDelta          = time.Millisecond * 500
	TimeoutCommit                  = time.Millisecond
	DelayedPrecommitTimeout        = time.Millisecond * 5850

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
	// MempoolSize determines the default max mempool size.
	MempoolSize = 400 * mebibyte // 400 MiB
	// UnbondingTime is the time a validator must wait to unbond in a proof of
	// stake system. Any validator within this time can be subject to slashing
	// under conditions of misbehavior.
	//
	// Modified from 3 weeks to 14 days + 1 hour in CIP-037.
	UnbondingTime = 337 * time.Hour // (14 days + 1 hour)

)

// MinCommissionRate is 10%. It is the minimum commission rate for a validator
// as defined in CIP-41.
var MinCommissionRate = math.LegacyNewDecWithPrec(1, 1)
