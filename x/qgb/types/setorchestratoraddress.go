package types

import sdk "github.com/cosmos/cosmos-sdk/types"

// GetSigners defines whose signature is required
func (msg *MsgSetOrchestratorAddress) GetSigners() []sdk.AccAddress {
	// TODO
	return []sdk.AccAddress{}
}

// ValidateBasic
func (msg *MsgSetOrchestratorAddress) ValidateBasic() error {
	// TODO
	return nil
}
