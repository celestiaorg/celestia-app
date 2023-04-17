package appconsts

import (
	"github.com/celestiaorg/rsmt2d"
	"github.com/tendermint/tendermint/pkg/consts"
)

// These constants were originally sourced from:
// https://github.com/celestiaorg/celestia-specs/blob/master/src/specs/consensus.md#constants
const (
	// NamespaveVersionSize is the size of a namespace version in bytes.
	NamespaceVersionSize = 1

	// NamespaceIDSize is the size of a namespace ID in bytes.
	NamespaceIDSize = 32

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

	// DefaultMaxSquareSize is the maximum original square width.
	//
	// Note: 128 shares in a row * 128 shares in a column * 512 bytes in a share
	// = 8 MiB
	DefaultMaxSquareSize = 128

	// MaxShareCount is the maximum number of shares allowed in the original
	// data square.
	MaxShareCount = DefaultMaxSquareSize * DefaultMaxSquareSize

	// DefaultMinSquareSize is the smallest original square width.
	DefaultMinSquareSize = 1

	// MinshareCount is the minimum number of shares allowed in the original
	// data square.
	MinShareCount = DefaultMinSquareSize * DefaultMinSquareSize

	// SubtreeRootThreshold dictates the threashold for increasing the sub tree
	// root height for blobs. If the number of subtree roots used to create a
	// share commitment surpasses this threshold, then the height is increased.
	// The rationale for this value is described in more detail in ADR013
	// (./docs/architecture/adr-013). Note that this value should not drop below
	// the max square size because that would result in sub tree widths that are
	// larger than the actual suquare size for that block. Having a value that
	// is larger than the square is simply wasteful in that the proofs for large
	// blobs are unnecessarily large.
	// ADR013 https://github.com/celestiaorg/celestia-app/blob/e905143e8fe138ce6085ae9a5c1af950a2d87638/docs/architecture/adr-013-non-interactive-default-rules-for-zero-padding.md //nolint: lll
	SubtreeRootThreshold = DefaultMaxSquareSize

	// MaxShareVersion is the maximum value a share version can be.
	MaxShareVersion = 127

	// DefaultGasPerBlobByte is the default gas cost deducted per byte of blob
	// included in a PayForBlobs txn
	DefaultGasPerBlobByte = 8

	// TransactionsPerBlockLimit is the maximum number of transactions a block
	// producer will include in a block.
	//
	// NOTE: Currently this value is set at roughly the number of PFBs that
	// would fill one quarter of the max square size.
	TransactionsPerBlockLimit = 5090
)

var (
	// NewBaseHashFunc is the base hash function used by NMT. Change accordingly
	// if another hash.Hash should be used as a base hasher in the NMT.
	NewBaseHashFunc = consts.NewBaseHashFunc

	// DefaultCodec is the default codec creator used for data erasure.
	DefaultCodec = rsmt2d.NewLeoRSCodec

	// DataCommitmentBlocksLimit is the limit to the number of blocks we can
	// generate a data commitment for.
	DataCommitmentBlocksLimit = consts.DataCommitmentBlocksLimit

	// SupportedShareVersions is a list of supported share versions.
	SupportedShareVersions = []uint8{ShareVersionZero}
)
