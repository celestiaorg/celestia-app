package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
)

// RegisterLegacyAminoCodec registers the upgrade types on the LegacyAmino codec.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(upgradetypes.Plan{}, "cosmos-sdk/Plan", nil)
}

// RegisterInterfaces registers the upgrade module types.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
