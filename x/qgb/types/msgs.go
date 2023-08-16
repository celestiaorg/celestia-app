package types

import (
	"fmt"

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

	// assert that the length of the EVM address is correct
	if len(msg.EvmAddress) != common.AddressLength*2+2 {
		return fmt.Errorf("EVM address must be %d bytes long got %d", common.AddressLength*2+2, len(msg.EvmAddress))
	}

	return nil
}

// GetSigner fulfills the sdk.Msg interface. The signer must be the validator address
func (msg MsgRegisterEVMAddress) GetSigners() []sdk.AccAddress {
	valAddr, err := sdk.ValAddressFromBech32(msg.ValidatorAddress)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{sdk.AccAddress(valAddr)}
}
