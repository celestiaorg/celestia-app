package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// NewMsgDataCommitmentConfirm creates a new NewMsgDataCommitmentConfirm
func NewMsgDataCommitmentConfirm(
	commitment string,
	signature string,
	validatorSignature string,
	beginBlock int64,
	endBlock int64,
) *MsgDataCommitmentConfirm {
	return &MsgDataCommitmentConfirm{
		Commitment:       commitment,
		Signature:        signature,
		ValidatorAddress: validatorSignature,
		BeginBlock:       beginBlock,
		EndBlock:         endBlock,
	}
}

// GetSigners defines whose signature is required
func (msg *MsgDataCommitmentConfirm) GetSigners() []sdk.AccAddress {
	acc, err := sdk.AccAddressFromBech32(msg.ValidatorAddress)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{acc}
}

// ValidateBasic performs stateless checks
func (msg *MsgDataCommitmentConfirm) ValidateBasic() (err error) {
	if _, err = sdk.AccAddressFromBech32(msg.ValidatorAddress); err != nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidAddress, msg.ValidatorAddress)
	}
	if msg.BeginBlock > msg.EndBlock {
		return sdkerrors.Wrap(err, "begin block should be less than end block")
	}
	// FIXME: add `that the provided validator's bridge address is the one associated with that validator`
	return nil
}
