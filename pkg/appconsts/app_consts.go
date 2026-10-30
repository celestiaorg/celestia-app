package appconsts

import (
	"time"

	"cosmossdk.io/math"
)

const (
	// Version is the current application version.
	Version uint64 = 11
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
	//
	// WARNING: DO NOT MODIFY this value without cross-team coordination.
	// SubtreeRootThreshold is hard-coded in clients (e.g. Lumina). Changing this
	// value is a breaking change for those clients. Client upgrades are difficult
	// to coordinate because they are deployed across many environments. At a
	// minimum, changing SubtreeRootThreshold requires a new client release and
	// deployment before the change takes effect on the network.
	// See: https://github.com/celestiaorg/celestia-app/issues/6831
	SubtreeRootThreshold int    = 64
	TxSizeCostPerByte    uint64 = 10
	GasPerBlobByte       uint32 = 8
	MaxTxSize            int    = 8_388_608 // 8 MiB in bytes
	// MaxExpectedTimePerBlock is the IBC connection parameter that should be
	// 3-5x the expected block time. From v11 the dynamic timeout commit floors
	// the block interval at 800ms, so this value is 4 seconds (5x).
	MaxExpectedTimePerBlock = 4 * time.Second

	// The block counts below are the v10 values x3.25, the ratio of the v10
	// (~2.6s) to the v11 (0.8s) block interval, so each keeps its duration.

	// SignedBlocksWindow is the x/slashing liveness window (v10: 10,000).
	SignedBlocksWindow int64 = 32_500
	// HistoricalEntries is the number of x/staking historical infos kept. It
	// must cover the fibre PaymentPromiseHeightWindow (v10: 10,000).
	HistoricalEntries uint32 = 32_500
	// MempoolTTLNumBlocks is the minimum mempool TTL in blocks (v10: 36).
	MempoolTTLNumBlocks int64 = 117
	// SnapshotInterval is the default state sync snapshot interval in blocks
	// (v10: 1,500).
	SnapshotInterval uint64 = 4_875

	// TestUpgradeHeightDelay is the number of blocks that chain-id "test" waits
	// after a MsgTryUpgrade to activate the next version.
	TestUpgradeHeightDelay = int64(3)
	// CortoUpgradeHeightDelay is the number of blocks that Corto waits
	// after a MsgTryUpgrade to activate the next version. Assuming a block
	// interval of ~0.8 seconds, this is 1 day.
	CortoUpgradeHeightDelay = int64(108_000)
	// MochaUpgradeHeightDelay is the number of blocks that Mocha waits
	// after a MsgTryUpgrade to activate the next version. Assuming a block
	// interval of ~0.8 seconds, this is 2 days.
	MochaUpgradeHeightDelay = int64(216_000)
	// MainnetUpgradeHeightDelay is the number of blocks that Mainnet waits
	// after a MsgTryUpgrade to activate the next version. Assuming a block
	// interval of ~0.8 seconds, this is 7 days.
	MainnetUpgradeHeightDelay = int64(756_000)
	// MempoolSize determines the default max mempool size.
	MempoolSize = 400 * mebibyte // 400 MiB
	// UnbondingTime is the time a validator must wait to unbond in a proof of
	// stake system. Any validator within this time can be subject to slashing
	// under conditions of misbehavior.
	//
	// Modified from 3 weeks to 14 days + 1 hour in CIP-037.
	UnbondingTime = 337 * time.Hour // (14 days + 1 hour)

	// BlockMaxBytes is the governance-modifiable max number of bytes in a block.
	// Set via the v9 upgrade handler.
	BlockMaxBytes = 32 * mebibyte

	// GasPerISMSignature is charged per Hyperlane ISM signature, pricing one
	// secp256k1 recovery (~170 us pure Go at ~4.7 ns per gas).
	GasPerISMSignature uint64 = 36_000

	// MaxMultisigISMValidators bounds the validator set of a Hyperlane multisig
	// ISM, which every verification decodes in full regardless of threshold.
	MaxMultisigISMValidators = 100
)

// MinCommissionRate is 20%.
// TODO(@rootulp): link to CIP.
var MinCommissionRate = math.LegacyNewDecWithPrec(2, 1)
