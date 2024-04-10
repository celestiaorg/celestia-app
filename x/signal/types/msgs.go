package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/migrations/legacytx"
)

const (
	ModuleName = "signal"

	StoreKey     = ModuleName
	QuerierRoute = ModuleName
	RouterKey    = ModuleName

	URLMsgSignalVersion = "/celestia.signal.v1.Msg/SignalVersion"
	URLMsgTryUpgrade    = "/celestia.signal_test.v1.Msg/TryUpgrade"
)

var (
	_ sdk.Msg            = &MsgSignalVersion{}
	_ sdk.Msg            = &MsgTryUpgrade{}
	_ legacytx.LegacyMsg = &MsgSignalVersion{}
	_ legacytx.LegacyMsg = &MsgTryUpgrade{}
)

var ModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())

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

// GetSignBytes implements legacytx.LegacyMsg.
func (msg *MsgSignalVersion) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg))
}

// Route implements legacytx.LegacyMsg.
func (msg *MsgSignalVersion) Route() string {
	return RouterKey
}

// Type implements legacytx.LegacyMsg.
func (msg *MsgSignalVersion) Type() string {
	return URLMsgSignalVersion
}

func NewMsgTryUpgrade(signer sdk.AccAddress) *MsgTryUpgrade {
	return &MsgTryUpgrade{
		Signer: signer.String(),
	}
}

func (msg *MsgTryUpgrade) GetSigners() []sdk.AccAddress {
	addr, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{addr}
}

func (msg *MsgTryUpgrade) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(msg.Signer)
	return err
}

// GetSignBytes implements legacytx.LegacyMsg.
func (msg *MsgTryUpgrade) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg))
}

// Route implements legacytx.LegacyMsg.
func (msg *MsgTryUpgrade) Route() string {
	return RouterKey
}

// Type implements legacytx.LegacyMsg.
func (msg *MsgTryUpgrade) Type() string {
	return URLMsgTryUpgrade
}
