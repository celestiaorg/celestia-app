package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
)

const (
	StoreKey = upgradetypes.StoreKey

	// Copied from cosmos/cosmos-sdk/x/upgrade/types/keys.go:
	ModuleName = upgradetypes.ModuleName

	// QuerierRoute defines the module's query routing key
	QuerierRoute = ModuleName
)

var (
	_ sdk.Msg = &MsgSignalVersion{}
	_ sdk.Msg = &MsgTryUpgrade{}
)

func NewMsgSignalVersion(valAddress sdk.ValAddress, version uint64) *MsgSignalVersion {
	return &MsgSignalVersion{
		ValidatorAddress: valAddress.String(),
		Version:          version,
	}
}

func (msg *MsgSignalVersion) GetSigners() []sdk.AccAddress {
	valAddr, err := sdk.ValAddressFromBech32(msg.ValidatorAddress)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{sdk.AccAddress(valAddr)}
}

func (msg *MsgSignalVersion) ValidateBasic() error {
	_, err := sdk.ValAddressFromBech32(msg.ValidatorAddress)
	return err
}

func NewMsgTryUpgrade(signer sdk.AccAddress) *MsgTryUpgrade {
	return &MsgTryUpgrade{
		Signer: signer.String(),
	}
}

func (msg *MsgTryUpgrade) GetSigners() []sdk.AccAddress {
	valAddr, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{sdk.AccAddress(valAddr)}
}

func (msg *MsgTryUpgrade) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(msg.Signer)
	return err
}
