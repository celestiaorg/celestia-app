package appconsts

import (
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/celestiaorg/rsmt2d"
	"github.com/tendermint/tendermint/pkg/consts"
)

// These constants were originally sourced from:
// https://github.com/celestiaorg/celestia-specs/blob/master/src/specs/consensus.md#constants
//
// They can not change throughout the lifetime of a network.
const (
	// DefaultShareVersion is the defacto share version. Use this if you are
	// unsure of which version to use.
	DefaultShareVersion = share.ShareVersionZero

	// MinSquareSize is the smallest original square width.
	MinSquareSize = 1

	// MinshareCount is the minimum number of shares allowed in the original
	// data square.
	MinShareCount = MinSquareSize * MinSquareSize

	// BondDenom defines the native staking denomination
	BondDenom = "utia"
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

// HashLength returns the length of a hash in bytes.
func HashLength() int {
	return hashLength
}
