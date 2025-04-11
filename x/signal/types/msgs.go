package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	ModuleName = "signal"
	StoreKey   = ModuleName

	URLMsgSignalVersion = "/celestia.signal.v1.Msg/SignalVersion"
	URLMsgTryUpgrade    = "/celestia.signal.v1.Msg/TryUpgrade"
)

var (
	_ sdk.Msg = &MsgSignalVersion{}
	_ sdk.Msg = &MsgTryUpgrade{}
)

var ModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())

func NewMsgSignalVersion(valAddress string, version uint64) *MsgSignalVersion {
	return &MsgSignalVersion{
		ValidatorAddress: valAddress,
		Version:          version,
	}
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

func (msg *MsgTryUpgrade) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(msg.Signer)
	return err
}
