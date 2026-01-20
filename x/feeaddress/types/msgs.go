package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ sdk.Msg = &MsgForwardFees{}

// NewMsgForwardFees creates a new MsgForwardFees.
func NewMsgForwardFees() *MsgForwardFees {
	return &MsgForwardFees{}
}

// ValidateBasic performs basic validation on the message.
// Note: This message is special - it has no signer and no fields. It is injected
// by the protocol. Validation happens via ProcessProposal checking tx fee == fee address balance.
func (msg *MsgForwardFees) ValidateBasic() error {
	return nil
}

// IsFeeForwardMsg checks if a transaction contains exactly one MsgForwardFees message.
// Returns the message if found, nil otherwise. This is the canonical helper for
// detecting fee forward transactions and should be used instead of duplicating
// this logic across the codebase.
func IsFeeForwardMsg(tx sdk.Tx) *MsgForwardFees {
	msgs := tx.GetMsgs()
	if len(msgs) != 1 {
		return nil
	}
	msg, ok := msgs[0].(*MsgForwardFees)
	if !ok {
		return nil
	}
	return msg
}
