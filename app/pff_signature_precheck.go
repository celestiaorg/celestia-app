package app

import (
	"bytes"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	squaretx "github.com/celestiaorg/go-square/v4/tx"
	coretypes "github.com/cometbft/cometbft/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"google.golang.org/protobuf/encoding/protowire"
)

const (
	wireTxRawBody                 = 1 // TxRaw.body_bytes
	wireTxBodyMessages            = 1 // TxBody.messages
	wireAnyTypeURL                = 1 // Any.type_url
	wireAnyValue                  = 2 // Any.value
	wirePFFValidatorSignatures    = 3 // MsgPayForFibre.validator_signatures
	wireMsgExecMsgs               = 2 // MsgExec.msgs
	wireMsgSubmitProposalMessages = 1 // MsgSubmitProposal.messages
)

var (
	msgPayForFibreTypeURLBytes = []byte(fibretypes.MsgPayForFibreTypeURL)
	indexWrapperTypeIDBytes    = []byte(squaretx.ProtoIndexWrapperTypeID)
	msgExecTypeURL             = sdk.MsgTypeURL(&authz.MsgExec{})
	msgSubmitProposalTypeURL   = sdk.MsgTypeURL(&govv1.MsgSubmitProposal{})
)

// exceedsPFFSignatureLimit reports whether the SDK tx bytes carry a
// MsgPayForFibre with more validator signatures than ValidateBasic accepts. It
// walks the wire format without allocating, so CheckTx can reject such a tx
// before the decoder materializes every entry.
//
// It never rejects a tx that would pass decoding and ValidateBasic: it counts
// exactly the entries the decoder would, and a MsgPayForFibre nested in a
// MsgExec or MsgSubmitProposal is rejected by ante anyway. Like the decoder's
// unknown field check, it skips other fields with ConsumeFieldValue, groups
// included. Input that check cannot parse reports false: the decoder rejects
// it before unmarshalling any entries.
func exceedsPFFSignatureLimit(raw []byte) bool {
	// The type URL is a verbatim subrange of any tx carrying a MsgPayForFibre.
	if !bytes.Contains(raw, msgPayForFibreTypeURLBytes) {
		return false
	}
	// Unwrap an IndexWrapper exactly when the tx decoder does.
	if bytes.Contains(raw, indexWrapperTypeIDBytes) {
		if indexWrapper, ok := coretypes.UnmarshalIndexWrapper(raw); ok {
			raw = indexWrapper.Tx
		}
	}
	// A repeated body_bytes resolves to the last occurrence, as in the decoder.
	body, ok := lastBytesField(raw, wireTxRawBody)
	if !ok {
		return false
	}
	return anysExceedPFFSignatureLimit(body, wireTxBodyMessages, codectypes.MaxUnpackAnyRecursionDepth)
}

// anysExceedPFFSignatureLimit scans the Any entries of field in msg.
func anysExceedPFFSignatureLimit(msg []byte, field protowire.Number, depth int) bool {
	if depth <= 0 {
		return false
	}
	for len(msg) > 0 {
		num, typ, n := protowire.ConsumeTag(msg)
		if n < 0 {
			return false
		}
		msg = msg[n:]
		if num == field && typ == protowire.BytesType {
			anyBz, n := protowire.ConsumeBytes(msg)
			if n < 0 {
				return false
			}
			msg = msg[n:]
			if anyExceedsPFFSignatureLimit(anyBz, depth) {
				return true
			}
			continue
		}
		n = protowire.ConsumeFieldValue(num, typ, msg)
		if n < 0 {
			return false
		}
		msg = msg[n:]
	}
	return false
}

func anyExceedsPFFSignatureLimit(anyBz []byte, depth int) bool {
	typeURL, ok := lastBytesField(anyBz, wireAnyTypeURL)
	if !ok {
		return false
	}
	value, _ := lastBytesField(anyBz, wireAnyValue)
	// Comparing string(typeURL) directly, rather than switching on it, does not allocate.
	switch {
	case string(typeURL) == fibretypes.MsgPayForFibreTypeURL:
		return countBytesField(value, wirePFFValidatorSignatures, appconsts.MaxFibreValidatorSignatures+1) > appconsts.MaxFibreValidatorSignatures
	case string(typeURL) == msgExecTypeURL:
		return anysExceedPFFSignatureLimit(value, wireMsgExecMsgs, depth-1)
	case string(typeURL) == msgSubmitProposalTypeURL:
		return anysExceedPFFSignatureLimit(value, wireMsgSubmitProposalMessages, depth-1)
	}
	return false
}

// countBytesField counts the length-delimited entries of field in msg, stopping
// at limit or at the first malformed field.
func countBytesField(msg []byte, field protowire.Number, limit int) int {
	count := 0
	for len(msg) > 0 && count < limit {
		num, typ, n := protowire.ConsumeTag(msg)
		if n < 0 {
			return count
		}
		msg = msg[n:]
		if n = protowire.ConsumeFieldValue(num, typ, msg); n < 0 {
			return count
		}
		msg = msg[n:]
		if num == field && typ == protowire.BytesType {
			count++
		}
	}
	return count
}

// lastBytesField returns the last length-delimited value of field in msg, and
// false if msg is malformed or has none.
func lastBytesField(msg []byte, field protowire.Number) ([]byte, bool) {
	var (
		value []byte
		found bool
	)
	for len(msg) > 0 {
		num, typ, n := protowire.ConsumeTag(msg)
		if n < 0 {
			return nil, false
		}
		msg = msg[n:]
		if num == field && typ == protowire.BytesType {
			v, n := protowire.ConsumeBytes(msg)
			if n < 0 {
				return nil, false
			}
			value, found = v, true
			msg = msg[n:]
			continue
		}
		if n = protowire.ConsumeFieldValue(num, typ, msg); n < 0 {
			return nil, false
		}
		msg = msg[n:]
	}
	return value, found
}
