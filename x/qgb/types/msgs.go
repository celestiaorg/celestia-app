package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

var _ sdk.Msg = &MsgRegisterEVMAddress{}

func NewMsgRegisterEVMAddress(valAddress, evmAddress string) (*MsgRegisterEVMAddress, error) {
	msg := &MsgRegisterEVMAddress{
		ValidatorAddress: valAddress,
		EvmAddress:       evmAddress,
	}
	return msg, msg.ValidateBasic()
}

// ValidateBasic verifies that the EVM address and val address are of a valid type
func (msg MsgRegisterEVMAddress) ValidateBasic() error {
	_, err := sdk.ValAddressFromBech32(msg.ValidatorAddress)
	if err != nil {
		return err
	}

	if !common.IsHexAddress(msg.EvmAddress) {
		return ErrEVMAddressNotHex
	}

	return nil
}

// GetSigner fulfills the sdk.Msg interface. The signer must be the validator address
func (msg MsgRegisterEVMAddress) GetSigners() []sdk.AccAddress {
	address, err := sdk.AccAddressFromBech32(msg.ValidatorAddress)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{address}
}
