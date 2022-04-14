package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var _ sdk.Msg = &MsgValsetConfirm{}

// NewMsgValsetConfirm returns a new msgValSetConfirm
func NewMsgValsetConfirm(
	nonce uint64,
	ethAddress EthAddress,
	validator sdk.AccAddress,
	signature string,
) *MsgValsetConfirm {
	return &MsgValsetConfirm{
		Nonce:        nonce,
		Orchestrator: validator.String(),
		EthAddress:   ethAddress.GetAddress(),
		Signature:    signature,
	}
}

// GetSigners defines whose signature is required
func (msg *MsgValsetConfirm) GetSigners() []sdk.AccAddress {
	// TODO: figure out how to convert between AccAddress and ValAddress properly
	acc, err := sdk.AccAddressFromBech32(msg.Orchestrator)
	if err != nil {
		panic(err)
	}

	return []sdk.AccAddress{acc}
}

// ValidateBasic performs stateless checks
func (msg *MsgValsetConfirm) ValidateBasic() (err error) {
	if _, err = sdk.AccAddressFromBech32(msg.Orchestrator); err != nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidAddress, msg.Orchestrator)
	}
	if err := ValidateEthAddress(msg.EthAddress); err != nil {
		return sdkerrors.Wrap(err, "ethereum address")
	}
	return nil
}

// Type should return the action
func (msg *MsgValsetConfirm) Type() string { return "valset_confirm" }
