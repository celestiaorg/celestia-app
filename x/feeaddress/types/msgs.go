package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	URLMsgForwardFees = "/celestia.feeaddress.v1.Msg/ForwardFees"
)

var _ sdk.Msg = &MsgForwardFees{}

// NewMsgForwardFees creates a new MsgForwardFees.
// The proposer should be the hex-encoded address of the block proposer.
func NewMsgForwardFees(proposer string) *MsgForwardFees {
	return &MsgForwardFees{
		Proposer: proposer,
	}
}

// ValidateBasic performs basic validation on the message.
// Note: This message is special - it has no signer and is injected by the protocol.
// Full validation (proposer matching block proposer) happens in ProcessProposal.
func (msg *MsgForwardFees) ValidateBasic() error {
	if msg.Proposer == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "proposer cannot be empty")
	}
	return nil
}
