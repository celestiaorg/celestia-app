package appconsts

import (
	"strconv"

	"github.com/celestiaorg/go-square/v2/share"
	"github.com/celestiaorg/rsmt2d"
	"github.com/tendermint/tendermint/pkg/consts"
)

// These constants were originally sourced from:
// https://github.com/celestiaorg/celestia-specs/blob/master/src/specs/consensus.md#constants
//
// They cannot change throughout the lifetime of a network.
const (
	// DefaultShareVersion is the defacto share version. Use this if you are
	// unsure of which version to use.
	DefaultShareVersion = share.ShareVersionZero

	// MinSquareSize is the smallest original square width.
	MinSquareSize = 1

	// MinShareCount is the minimum number of shares allowed in the original
	// data square.
	MinShareCount = MinSquareSize * MinSquareSize

	// BondDenom defines the native staking denomination
	BondDenom = "utia"

	// DefaultUpgradeHeightDelay is the number of blocks after a quorum has been
	// reached that the chain should upgrade to the new version. Assuming a block
	// interval of 12 seconds, this is 7 days.
	DefaultUpgradeHeightDelay = int64(7 * 24 * 60 * 60 / 12) // 7 days * 24 hours * 60 minutes * 60 seconds / 12 seconds per block = 50,400 blocks.
)

var (
	// DataCommitmentBlocksLimit is the maximum number of blocks that a data commitment can span
	DataCommitmentBlocksLimit = consts.DataCommitmentBlocksLimit

	// NewBaseHashFunc is the base hash function used by NMT. Change accordingly
	// if another hash.Hash should be used as a base hasher in the NMT.
	NewBaseHashFunc = consts.NewBaseHashFunc

	// hashLength is the length of a hash in bytes.
	hashLength = NewBaseHashFunc().Size()

	// DefaultCodec is the default codec creator used for data erasure.
	DefaultCodec = rsmt2d.NewLeoRSCodec

	// SupportedShareVersions is a list of supported share versions.
	SupportedShareVersions = share.SupportedShareVersions
)

// UpgradeHeightDelay returns the delay in blocks after a quorum has been reached that the chain should upgrade to the new version.
func UpgradeHeightDelay() int64 {
	if OverrideUpgradeHeightDelayStr != "" {
		parsedValue, err := strconv.ParseInt(OverrideUpgradeHeightDelayStr, 10, 64)
		if err != nil {
			panic("Invalid OverrideUpgradeHeightDelayStr value")
		}
		return parsedValue
	}
	return DefaultUpgradeHeightDelay
}

// HashLength returns the length of a hash in bytes.
func HashLength() int {
	return hashLength
}

// The following consts are not consensus breaking and will be applied straight after this binary is started.
const (
	// NonPFBTransactionCap is the maximum number of SDK messages, aside from PFBs, that a block can contain.
	NonPFBTransactionCap = 200

	// PFBTransactionCap is the maximum number of PFB messages a block can contain.
	PFBTransactionCap = 600
)
