package appconsts

import (
	"bytes"
	"encoding/binary"

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
	// byte contains the share version and a start idicator.
	ShareInfoBytes = 1

	// ShareVersion is the current version of the share format
	ShareVersion = uint8(0)

	// CompactShareReservedBytes is the number of bytes reserved for the location of
	// the first unit (transaction, ISR, evidence) in a compact share.
	CompactShareReservedBytes = 2

	// ContinuationCompactShareContentSize is the number of bytes usable for
	// data in a continuation compact share. A continuation share is any
	// share in a reserved namespace that isn't the first share in that
	// namespace.
	ContinuationCompactShareContentSize = ShareSize - NamespaceSize - ShareInfoBytes - CompactShareReservedBytes

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

	// MaxShareVersion is the maximum value a share version can be.
	MaxShareVersion = 127

	// MalleatedTxBytes is the overhead bytes added to a normal transaction after
	// malleating it. 32 for the original hash, 4 for the uint32 share_index, and 3
	// for protobuf
	MalleatedTxBytes = 32 + 4 + 3

	// ShareCommitmentBytes is the number of bytes used by a protobuf encoded
	// share commitment. 64 bytes for the signature, 32 bytes for the
	// commitment, 8 bytes for the uint64, and 4 bytes for the protobuf overhead
	ShareCommitmentBytes = 64 + 32 + 8 + 4

	// MalleatedTxEstimateBuffer is the "magic" number used to ensure that the
	// estimate of a malleated transaction is at least as big if not larger than
	// the actual value. TODO: use a more accurate number
	MalleatedTxEstimateBuffer = 100
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
	// TODO(ismail): for better efficiency and a larger number shares
	// we should switch to the rsmt2d.LeopardFF16 codec:
	DefaultCodec = rsmt2d.NewRSGF8Codec

	// DataCommitmentBlocksLimit is the limit to the number of blocks we can generate a data commitment for.
	DataCommitmentBlocksLimit = consts.DataCommitmentBlocksLimit

	// NameSpacedPaddedShareBytes are the raw bytes that are used in the contents
	// of a NameSpacedPaddedShare. A NameSpacedPaddedShare follows a message so
	// that the next message starts at an index that conforms to non-interactive
	// defaults.
	NameSpacedPaddedShareBytes = bytes.Repeat([]byte{0}, SparseShareContentSize)

	// FirstCompactShareSequenceLengthBytes is the number of bytes reserved for the total
	// sequence length that is stored in the first compact share of a sequence. This
	// value is the maximum number of bytes required to store the sequence
	// length of a block that only contains shares of one type. For example, if
	// a block contains only evidence then it could contain: MaxSquareSize *
	// MaxSquareSize * ShareSize bytes of evidence.
	//
	// Assuming MaxSquareSize is 128 and ShareSize is 256, this is 4194304 bytes
	// of evidence. It takes 4 bytes to store a varint of 4194304.
	//
	// https://go.dev/play/p/MynwcDHQ_me
	FirstCompactShareSequenceLengthBytes = numberOfBytesVarint(MaxSquareSize * MaxSquareSize * ShareSize)

	// FirstCompactShareContentSize is the number of bytes usable for data in
	// the first compact share of a reserved namespace. This type of share
	// contains less space for data than a ContinuationCompactShare because the
	// first compact share includes a total sequence length varint.
	FirstCompactShareContentSize = ContinuationCompactShareContentSize - FirstCompactShareSequenceLengthBytes

	// SupportedShareVersions is a list of supported share versions.
	SupportedShareVersions = []uint8{ShareVersion}
)

// numberOfBytesVarint calculates the number of bytes needed to write a varint of n
func numberOfBytesVarint(n uint64) (numberOfBytes int) {
	buf := make([]byte, binary.MaxVarintLen64)
	return binary.PutUvarint(buf, n)
}
