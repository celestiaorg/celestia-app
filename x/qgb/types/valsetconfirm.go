package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetSigners defines whose signature is required
func (msg *MsgValsetConfirm) GetSigners() []sdk.AccAddress {
	// TODO: figure out how to convert between AccAddress and ValAddress properly
	acc, err := sdk.AccAddressFromBech32(msg.Orchestrator)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{acc}
}

// ValidateBasic
func (msg *MsgValsetConfirm) ValidateBasic() error {
	// TODO
	return nil
}
