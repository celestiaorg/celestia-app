package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers the upgrade types on the provided
// LegacyAmino codec.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgTryUpgrade{}, URLMsgTryUpgrade, nil)
	cdc.RegisterConcrete(&MsgSignalVersion{}, URLMsgSignalVersion, nil)
}

// RegisterInterfaces registers the signal module types on the provided
// registry.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil), &MsgTryUpgrade{})
	registry.RegisterImplementations((*sdk.Msg)(nil), &MsgSignalVersion{})
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
