package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
)

// RegisterLegacyAminoCodec registers the module's concrete types on the
// legacy Amino codec.
func RegisterLegacyAminoCodec(_ *codec.LegacyAmino) {}

// RegisterInterfaces registers module interfaces.
func RegisterInterfaces(_ cdctypes.InterfaceRegistry) {}
