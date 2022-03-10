package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// NewMsgSetOrchestratorAddress returns a new msgSetOrchestratorAddress
func NewMsgSetOrchestratorAddress(val sdk.ValAddress, oper sdk.AccAddress, eth EthAddress) *MsgSetOrchestratorAddress {
	return &MsgSetOrchestratorAddress{
		Validator:    val.String(),
		Orchestrator: oper.String(),
		EthAddress:   eth.GetAddress(),
	}
}

// GetSigners defines whose signature is required
func (msg *MsgSetOrchestratorAddress) GetSigners() []sdk.AccAddress {
	acc, err := sdk.ValAddressFromBech32(msg.Validator)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{sdk.AccAddress(acc)}
}

// ValidateBasic performs stateless checks
func (msg *MsgSetOrchestratorAddress) ValidateBasic() (err error) {
	if _, err = sdk.ValAddressFromBech32(msg.Validator); err != nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidAddress, msg.Validator)
	}
	if _, err = sdk.AccAddressFromBech32(msg.Orchestrator); err != nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidAddress, msg.Orchestrator)
	}
	if err := ValidateEthAddress(msg.EthAddress); err != nil {
		return sdkerrors.Wrap(err, "ethereum address")
	}
	return nil
}

// Type should return the action
func (msg *MsgSetOrchestratorAddress) Type() string { return "set_operator_address" }
