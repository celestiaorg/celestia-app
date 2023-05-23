package types

import (
	"time"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/gogo/protobuf/proto"
)

type AttestationType int64

const (
	DataCommitmentRequestType AttestationType = iota
	ValsetRequestType
)

// AttestationRequestI is either a DataCommitment or a Valset.
// This was decided as part of the universal nonce approach under:
// https://github.com/celestiaorg/celestia-app/issues/468#issuecomment-1156887715
type AttestationRequestI interface {
	proto.Message
	codec.ProtoMarshaler
	Type() AttestationType
	GetNonce() uint64
	BlockTime() time.Time
}
