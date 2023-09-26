package appconsts

import (
	"crypto/sha256"
	"math"

	"github.com/celestiaorg/rsmt2d"
)

// These constants were originally sourced from:
// https://github.com/celestiaorg/celestia-specs/blob/master/src/specs/consensus.md#constants
//
// They can not change throughout the lifetime of a network.
const (
	// NamespaceVersionSize is the size of a namespace version in bytes.
	NamespaceVersionSize = 1
	// NamespaceVersionMaxValue is the maximum value a namespace version can be.
	// This const must be updated if NamespaceVersionSize is changed.
	NamespaceVersionMaxValue = math.MaxUint8

	// NamespaceIDSize is the size of a namespace ID in bytes.
	NamespaceIDSize = 28

	// NamespaceSize is the size of a namespace (version + ID) in bytes.
	NamespaceSize = NamespaceVersionSize + NamespaceIDSize

	// ShareSize is the size of a share in bytes.
	ShareSize = 512

	// ShareInfoBytes is the number of bytes reserved for information. The info
	// byte contains the share version and a sequence start idicator.
	ShareInfoBytes = 1

	// SequenceLenBytes is the number of bytes reserved for the sequence length
	// that is present in the first share of a sequence.
	SequenceLenBytes = 4

	// ShareVersionZero is the first share version format.
	ShareVersionZero = uint8(0)

	// DefaultShareVersion is the defacto share version. Use this if you are
	// unsure of which version to use.
	DefaultShareVersion = ShareVersionZero

	// CompactShareReservedBytes is the number of bytes reserved for the location of
	// the first unit (transaction, ISR) in a compact share.
	CompactShareReservedBytes = 4

	// FirstCompactShareContentSize is the number of bytes usable for data in
	// the first compact share of a sequence.
	FirstCompactShareContentSize = ShareSize - NamespaceSize - ShareInfoBytes - SequenceLenBytes - CompactShareReservedBytes

	// ContinuationCompactShareContentSize is the number of bytes usable for
	// data in a continuation compact share of a sequence.
	ContinuationCompactShareContentSize = ShareSize - NamespaceSize - ShareInfoBytes - CompactShareReservedBytes

	// FirstSparseShareContentSize is the number of bytes usable for data in the
	// first sparse share of a sequence.
	FirstSparseShareContentSize = ShareSize - NamespaceSize - ShareInfoBytes - SequenceLenBytes

	// ContinuationSparseShareContentSize is the number of bytes usable for data
	// in a continuation sparse share of a sequence.
	ContinuationSparseShareContentSize = ShareSize - NamespaceSize - ShareInfoBytes

	// MinSquareSize is the smallest original square width.
	MinSquareSize = 1

	// MinshareCount is the minimum number of shares allowed in the original
	// data square.
	MinShareCount = MinSquareSize * MinSquareSize

	// MaxShareVersion is the maximum value a share version can be.
	MaxShareVersion = 127

	// BondDenom defines the native staking denomination
	BondDenom = "utia"
)

var (
	// NewBaseHashFunc is the base hash function used by NMT. Change accordingly
	// if another hash.Hash should be used as a base hasher in the NMT. NOTE:
	// this should always match the hash function defined in
	// celestia-core/pkg/consts. It is not imported here to avoid forcing users
	// of this module to also import celestia-core.
	NewBaseHashFunc = sha256.New

	// hashLength is the length of a hash in bytes.
	hashLength = NewBaseHashFunc().Size()

	// DefaultCodec is the default codec creator used for data erasure.
	DefaultCodec = rsmt2d.NewLeoRSCodec

	// SupportedShareVersions is a list of supported share versions.
	SupportedShareVersions = []uint8{ShareVersionZero}
)

func HashLength() int {
	return hashLength
}
