package appconsts

import (
	"bytes"
)

// These constants were originally sourced from:
// https://github.com/celestiaorg/celestia-specs/blob/master/src/specs/consensus.md#constants
const (
	// ShareSize is the size of a share in bytes.
	ShareSize = 256

	// NamespaceSize is the namespace size in bytes.
	NamespaceSize = 8

	// ShareInfoBytes is the number of bytes reserved for information. The info
	// byte contains the share version and a start idicator.
	ShareInfoBytes = 1

	// ShareVersion is the current version of the share format
	ShareVersion = uint8(0)

	// CompactShareReservedBytes is the number of bytes reserved for the location of
	// the first unit (transaction, ISR, evidence) in a compact share.
	CompactShareReservedBytes = 1

	// CompactShareContentSize is the number of bytes usable for data in a compact
	// (i.e. transactions, ISRs, evidence) share.
	CompactShareContentSize = ShareSize - NamespaceSize - ShareInfoBytes - CompactShareReservedBytes
	// SparseShareContentSize is the number of bytes usable for data in a sparse (i.e.
	// message) share.
	SparseShareContentSize = ShareSize - NamespaceSize - ShareInfoBytes

	// MaxSquareSize is the maximum number of
	// rows/columns of the original data shares in square layout.
	// Corresponds to AVAILABLE_DATA_ORIGINAL_SQUARE_MAX in the spec.
	// 128*128*256 = 4 Megabytes
	// TODO(ismail): settle on a proper max square
	// if the square size is larger than this, the block producer will panic
	MaxSquareSize = 128
	// MaxShareCount is the maximum number of shares allowed in the original data square.
	// if there are more shares than this, the block producer will panic.
	MaxShareCount = MaxSquareSize * MaxSquareSize

	// MinSquareSize depicts the smallest original square width. A square size smaller than this will
	// cause block producer to panic
	MinSquareSize = 1
	// MinshareCount is the minimum shares required in an original data square.
	MinShareCount = MinSquareSize * MinSquareSize
)

// MaxShareVersion is the maximum value a share version can be.
const MaxShareVersion = 127

var NameSpacedPaddedShareBytes = bytes.Repeat([]byte{0}, SparseShareContentSize)
