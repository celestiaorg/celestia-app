package types

import (
	"errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// NewMsgDataCommitmentConfirm creates a new NewMsgDataCommitmentConfirm
func NewMsgDataCommitmentConfirm(
	commitment string,
	signature string,
	validatorSignature sdk.AccAddress,
	ethAddress EthAddress,
	beginBlock int64,
	endBlock int64,
) *MsgDataCommitmentConfirm {
	return &MsgDataCommitmentConfirm{
		Commitment:       commitment,
		Signature:        signature,
		ValidatorAddress: validatorSignature.String(),
		EthAddress:       ethAddress.GetAddress(),
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
		return errors.New("begin block should be less than end block")
	}
	if err := ValidateEthAddress(msg.EthAddress); err != nil {
		return sdkerrors.Wrap(err, "ethereum address")
	}
	return nil
}

// Type should return the action
func (msg *MsgDataCommitmentConfirm) Type() string { return "data_commitment_confirm" }
