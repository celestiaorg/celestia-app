package appconsts

import (
	"bytes"

	"github.com/tendermint/tendermint/pkg/consts"
)

const (
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

var NameSpacedPaddedShareBytes = bytes.Repeat([]byte{0}, consts.MsgShareSize)
