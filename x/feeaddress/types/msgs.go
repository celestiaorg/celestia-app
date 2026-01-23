package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ sdk.Msg = &MsgPayProtocolFee{}

// NewMsgPayProtocolFee creates a new MsgPayProtocolFee with FromAddress set to the
// well-known fee address. The FromAddress field satisfies the signer annotation
// requirement, but signature verification is skipped because ProtocolFeeTerminatorDecorator
// terminates the ante chain before signature-related decorators run.
func NewMsgPayProtocolFee() *MsgPayProtocolFee {
	return &MsgPayProtocolFee{
		FromAddress: FeeAddressBech32,
	}
}

// IsProtocolFeeMsg checks if a transaction contains exactly one MsgPayProtocolFee message.
// Returns the message if found, nil otherwise. This is the canonical helper for
// detecting fee forward transactions and should be used instead of duplicating
// this logic across the codebase.
func IsProtocolFeeMsg(tx sdk.Tx) *MsgPayProtocolFee {
	msgs := tx.GetMsgs()
	if len(msgs) != 1 {
		return nil
	}
	msg, ok := msgs[0].(*MsgPayProtocolFee)
	if !ok {
		return nil
	}
	return msg
}
