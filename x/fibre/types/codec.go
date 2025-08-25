package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

var ModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgSetFibreProviderInfo{}, "fibre/SetFibreProviderInfo", nil)
	cdc.RegisterConcrete(&MsgRemoveFibreProviderInfo{}, "fibre/RemoveFibreProviderInfo", nil)
}

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgSetFibreProviderInfo{},
		&MsgRemoveFibreProviderInfo{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}