package upgrade

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
)

// TypeRegister is used to register the upgrade modules types in the encoding
// config without defining an entire module.
type TypeRegister struct{}

// RegisterLegacyAminoCodec registers the upgrade types on the LegacyAmino codec.
func (TypeRegister) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	upgradetypes.RegisterLegacyAminoCodec(cdc)
}

// RegisterInterfaces registers the upgrade module types.
func (TypeRegister) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	upgradetypes.RegisterInterfaces(registry)
}
