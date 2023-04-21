package upgrade

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
)

type TypeRegister struct {
}

// RegisterLegacyAminoCodec registers the upgrade types on the LegacyAmino codec
func (TypeRegister) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	upgradetypes.RegisterLegacyAminoCodec(cdc)
}

func (TypeRegister) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	upgradetypes.RegisterInterfaces(registry)
}
