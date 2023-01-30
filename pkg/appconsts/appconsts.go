package appconsts

import (
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
	// TxNamespaceID is the namespace reserved for transaction data.
	TxNamespaceID = consts.TxNamespaceID

	// IntermediateStateRootsNamespaceID is the namespace reserved for
	// intermediate state root data.
	// IntermediateStateRootsNamespaceID = namespace.ID{0, 0, 0, 0, 0, 0, 0, 2}

	// EvidenceNamespaceID is the namespace reserved for evidence.
	EvidenceNamespaceID = namespace.ID{0, 0, 0, 0, 0, 0, 0, 3}

	// PayForBlobNamespaceID is the namespace reserved for PayForBlobs transactions.
	PayForBlobNamespaceID = namespace.ID{0, 0, 0, 0, 0, 0, 0, 4}

	// ReservedNamespacePadding is the namespace used for padding after all
	// reserved namespaces. In practice this padding is after transactions
	// (ordinary and PFBs) but before blobs.
	ReservedNamespacePadding = namespace.ID{0, 0, 0, 0, 0, 0, 0, 255}

	// MaxReservedNamespace is the lexicographically largest namespace that is
	// reserved for protocol use.
	MaxReservedNamespace = namespace.ID{0, 0, 0, 0, 0, 0, 0, 255}

	// TailPaddingNamespaceID is the namespace reserved for tail padding. All data
	// with this namespace will be ignored.
	TailPaddingNamespaceID = namespace.ID{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFE}

	// ParitySharesNamespaceID is the namespace reserved for erasure coded data.
	ParitySharesNamespaceID = namespace.ID{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

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
