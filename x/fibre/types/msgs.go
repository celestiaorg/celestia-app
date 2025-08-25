package types

import (
	"strings"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	// MaxIPAddressLength defines the maximum allowed length for an IP address
	MaxIPAddressLength = 45
)

var (
	_ sdk.Msg = &MsgSetFibreProviderInfo{}
	_ sdk.Msg = &MsgRemoveFibreProviderInfo{}
)

// Route returns the message route for MsgSetFibreProviderInfo
func (m *MsgSetFibreProviderInfo) Route() string { return ModuleName }

// Type returns the message type for MsgSetFibreProviderInfo
func (m *MsgSetFibreProviderInfo) Type() string { return "set_fibre_provider_info" }

// GetSigners returns the signers for MsgSetFibreProviderInfo
func (m *MsgSetFibreProviderInfo) GetSigners() []sdk.AccAddress {
	valAddr, err := sdk.ValAddressFromBech32(m.ValidatorAddress)
	if err != nil {
		panic(err)
	}
	accAddr := sdk.AccAddress(valAddr)
	return []sdk.AccAddress{accAddr}
}

// GetSignBytes returns the sign bytes for MsgSetFibreProviderInfo
func (m *MsgSetFibreProviderInfo) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(m))
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

// Route returns the message route for MsgRemoveFibreProviderInfo
func (m *MsgRemoveFibreProviderInfo) Route() string { return ModuleName }

// Type returns the message type for MsgRemoveFibreProviderInfo
func (m *MsgRemoveFibreProviderInfo) Type() string { return "remove_fibre_provider_info" }

// GetSigners returns the signers for MsgRemoveFibreProviderInfo
func (m *MsgRemoveFibreProviderInfo) GetSigners() []sdk.AccAddress {
	addr, err := sdk.AccAddressFromBech32(m.RemoverAddress)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{addr}
}

// GetSignBytes returns the sign bytes for MsgRemoveFibreProviderInfo
func (m *MsgRemoveFibreProviderInfo) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(m))
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
