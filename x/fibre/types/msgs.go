package types

import (
	"strings"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	// MaxIPAddressLength defines the maximum allowed length for an IP address
	MaxIPAddressLength = 90
)

var (
	_ sdk.Msg = &MsgSetFibreProviderInfo{}
	_ sdk.Msg = &MsgRemoveFibreProviderInfo{}
)

// GetSigners returns the signers for MsgSetFibreProviderInfo
func (m *MsgSetFibreProviderInfo) GetSigners() []sdk.AccAddress {
	valAddr, err := sdk.ValAddressFromBech32(m.ValidatorAddress)
	if err != nil {
		panic(err)
	}
	accAddr := sdk.AccAddress(valAddr)
	return []sdk.AccAddress{accAddr}
}

// ValidateBasic performs basic validation for MsgSetFibreProviderInfo
func (m *MsgSetFibreProviderInfo) ValidateBasic() error {
	_, err := sdk.ValAddressFromBech32(m.ValidatorAddress)
	if err != nil {
		return errors.Wrapf(ErrInvalidValidatorAddress, "invalid validator address: %v", err)
	}

	if strings.TrimSpace(m.IpAddress) == "" {
		return ErrEmptyIPAddress
	}

	if len(m.IpAddress) > MaxIPAddressLength {
		return ErrIPAddressTooLong
	}

	return nil
}

// GetSigners returns the signers for MsgRemoveFibreProviderInfo
func (m *MsgRemoveFibreProviderInfo) GetSigners() []sdk.AccAddress {
	addr, err := sdk.AccAddressFromBech32(m.RemoverAddress)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{addr}
}

// ValidateBasic performs basic validation for MsgRemoveFibreProviderInfo
func (m *MsgRemoveFibreProviderInfo) ValidateBasic() error {
	_, err := sdk.ValAddressFromBech32(m.ValidatorAddress)
	if err != nil {
		return errors.Wrapf(ErrInvalidValidatorAddress, "invalid validator address: %v", err)
	}

	_, err = sdk.AccAddressFromBech32(m.RemoverAddress)
	if err != nil {
		return errors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid remover address: %v", err)
	}

	return nil
}
