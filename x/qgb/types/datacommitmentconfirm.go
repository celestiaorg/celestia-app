package types

import sdk "github.com/cosmos/cosmos-sdk/types"

// GetSigners defines whose signature is required
func (msg *MsgDataCommitmentConfirm) GetSigners() []sdk.AccAddress {
	// TODO
	return []sdk.AccAddress{}
}

// ValidateBasic
func (msg *MsgDataCommitmentConfirm) ValidateBasic() error {
	// TODO
	return nil
}
