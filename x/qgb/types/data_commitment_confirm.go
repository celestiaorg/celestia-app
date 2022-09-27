package types

import (
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	ethcmn "github.com/ethereum/go-ethereum/common"
)

// GetSigners defines whose signature is required.
func (msg *MsgDataCommitmentConfirm) GetSigners() []sdk.AccAddress {
	acc, err := sdk.AccAddressFromBech32(msg.ValidatorAddress)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{acc}
}

// ValidateBasic performs stateless checks.
func (msg *MsgDataCommitmentConfirm) ValidateBasic() (err error) {
	if _, err = sdk.AccAddressFromBech32(msg.ValidatorAddress); err != nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidAddress, msg.ValidatorAddress)
	}
	if msg.BeginBlock > msg.EndBlock {
		return sdkerrors.Wrap(ErrInvalid, "begin block should be less than end block")
	}
	if !ethcmn.IsHexAddress(msg.EthAddress) {
		return sdkerrors.Wrap(stakingtypes.ErrEthAddressNotHex, "ethereum address")
	}
	return nil
}

// Type should return the action.
func (msg *MsgDataCommitmentConfirm) Type() string { return "data_commitment_confirm" }
