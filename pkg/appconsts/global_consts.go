package appconsts

import (
	"crypto/sha256"

	"github.com/celestiaorg/go-square/v2/share"
	"github.com/celestiaorg/rsmt2d"
)

// These constants were originally sourced from:
// https://github.com/celestiaorg/celestia-specs/blob/master/src/specs/consensus.md#constants
//
// They cannot change throughout the lifetime of a network.
const (
	// DefaultShareVersion is the de facto share version. Use this if you are
	// unsure of which version to use.
	DefaultShareVersion = share.ShareVersionZero

	// MinSquareSize is the smallest original square width.
	MinSquareSize = 1

	// MinShareCount is the minimum number of shares allowed in the original
	// data square.
	MinShareCount = MinSquareSize * MinSquareSize

	// BondDenom defines the native staking denomination
	BondDenom = "utia"
)

var (
	// DataCommitmentBlocksLimit is the maximum number of blocks that a data commitment can span
	// DataCommitmentBlocksLimit is the limit to the number of blocks we can generate a data commitment for.
	// Deprecated: this is no longer used as we're moving towards Blobstream X. However, we're leaving it
	// here for backwards compatibility purpose until it's removed in the next breaking release.
	DataCommitmentBlocksLimit = 1000

	// NewBaseHashFunc is the base hash function used by NMT. Change accordingly
	// if another hash.Hash should be used as a base hasher in the NMT.
	NewBaseHashFunc = sha256.New

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
