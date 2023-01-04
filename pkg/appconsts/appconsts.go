package appconsts

import (
	"bytes"

	"github.com/celestiaorg/nmt"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/celestiaorg/rsmt2d"
	"github.com/tendermint/tendermint/pkg/consts"
)

// These constants were originally sourced from:
// https://github.com/celestiaorg/celestia-specs/blob/master/src/specs/consensus.md#constants
const (
	// ShareSize is the size of a share in bytes.
	ShareSize = 512

	// NamespaceSize is the namespace size in bytes.
	NamespaceSize = nmt.DefaultNamespaceIDLen

	// ShareInfoBytes is the number of bytes reserved for information. The info
	// byte contains the share version and a sequence start idicator.
	ShareInfoBytes = 1

	// SequenceLenBytes is the number of bytes reserved for the sequence length
	// that is present in the first share of a sequence.
	SequenceLenBytes = 4

	// ShareVersionZero is the first share version format.
	ShareVersionZero = uint8(0)

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

	// DefaultMaxSquareSize is the maximum number of
	// rows/columns of the original data shares in square layout.
	// Corresponds to AVAILABLE_DATA_ORIGINAL_SQUARE_MAX in the spec.
	// 128*128*256 = 4 Megabytes
	// TODO(ismail): settle on a proper max square
	// if the square size is larger than this, the block producer will panic
	DefaultMaxSquareSize = 128
	// MaxShareCount is the maximum number of shares allowed in the original data square.
	// if there are more shares than this, the block producer will panic.
	MaxShareCount = DefaultMaxSquareSize * DefaultMaxSquareSize

	// DefaultMinSquareSize depicts the smallest original square width. A square size smaller than this will
	// cause block producer to panic
	DefaultMinSquareSize = 1
	// MinshareCount is the minimum shares required in an original data square.
	MinShareCount = DefaultMinSquareSize * DefaultMinSquareSize

	// MaxShareVersion is the maximum value a share version can be.
	MaxShareVersion = 127

	// DefaultGasPerBlobByte is the default gas cost deducted per byte of blob
	// included in a PayForBlob txn
	DefaultGasPerBlobByte = 8
)

var (
	// TxNamespaceID is the namespace reserved for transaction data
	TxNamespaceID = consts.TxNamespaceID

	// IntermediateStateRootsNamespaceID is the namespace reserved for
	// intermediate state root data
	// TODO(liamsi): code commented out but kept intentionally.
	// IntermediateStateRootsNamespaceID = namespace.ID{0, 0, 0, 0, 0, 0, 0, 2}

	// EvidenceNamespaceID is the namespace reserved for evidence
	EvidenceNamespaceID = namespace.ID{0, 0, 0, 0, 0, 0, 0, 3}

	// MaxReservedNamespace is the lexicographically largest namespace that is
	// reserved for protocol use. It is derived from NAMESPACE_ID_MAX_RESERVED
	// https://github.com/celestiaorg/celestia-specs/blob/master/src/specs/consensus.md#constants
	MaxReservedNamespace = namespace.ID{0, 0, 0, 0, 0, 0, 0, 255}
	// TailPaddingNamespaceID is the namespace ID for tail padding. All data
	// with this namespace will be ignored
	TailPaddingNamespaceID = namespace.ID{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFE}
	// ParitySharesNamespaceID indicates that share contains erasure data
	ParitySharesNamespaceID = namespace.ID{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

	// NewBaseHashFunc change accordingly if another hash.Hash should be used as a base hasher in the NMT:
	NewBaseHashFunc = consts.NewBaseHashFunc

	// DefaultCodec is the default codec creator used for data erasure
	DefaultCodec = rsmt2d.NewLeoRSCodec

	// DataCommitmentBlocksLimit is the limit to the number of blocks we can generate a data commitment for.
	DataCommitmentBlocksLimit = consts.DataCommitmentBlocksLimit

	// NameSpacedPaddedShareBytes are the raw bytes that are used in the contents
	// of a NameSpacedPaddedShare. A NameSpacedPaddedShare follows a blob so
	// that the next blob starts at an index that conforms to non-interactive
	// defaults.
	NameSpacedPaddedShareBytes = bytes.Repeat([]byte{0}, FirstSparseShareContentSize)

	// SupportedShareVersions is a list of supported share versions.
	SupportedShareVersions = []uint8{ShareVersionZero}
)
