package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
)

// RegisterLegacyAminoCodec registers the upgrade types on the LegacyAmino codec.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(upgradetypes.Plan{}, "cosmos-sdk/Plan", nil)
	cdc.RegisterConcrete(&MsgTryUpgrade{}, "/celestia.upgrade.v1.Msg/TryUpgrade", nil)
	cdc.RegisterConcrete(&MsgSignalVersion{}, "/celestia.upgrade.v1.Msg/SignalVersion", nil)
}

// RegisterInterfaces registers the upgrade module types.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil), &MsgTryUpgrade{})
	registry.RegisterImplementations((*sdk.Msg)(nil), &MsgSignalVersion{})
	registry.RegisterInterface("cosmos.auth.v1beta1.BaseAccount", (*authtypes.AccountI)(nil))
	registry.RegisterImplementations((*authtypes.AccountI)(nil), &authtypes.BaseAccount{})
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
