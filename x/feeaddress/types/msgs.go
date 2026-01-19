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
