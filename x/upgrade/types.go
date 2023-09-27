package upgrade

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
)

const (
	StoreKey   = upgradetypes.StoreKey
	ModuleName = upgradetypes.ModuleName
)

// TypeRegister is used to register the upgrade module's types in the encoding
// config without defining an entire module.
type TypeRegister struct{}

// RegisterLegacyAminoCodec registers the upgrade types on the LegacyAmino codec.
func (TypeRegister) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(upgradetypes.Plan{}, "cosmos-sdk/Plan", nil)
}

// RegisterInterfaces registers the upgrade module types.
func (TypeRegister) RegisterInterfaces(_ codectypes.InterfaceRegistry) {}
