package encoding

import (
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/std"
	"github.com/cosmos/cosmos-sdk/x/auth/tx"
)

type ModuleRegister interface {
	RegisterLegacyAminoCodec(*codec.LegacyAmino)
	RegisterInterfaces(codectypes.InterfaceRegistry)
}

// Config specifies the concrete encoding types to use for a given app.
// This is provided for compatibility between protobuf and amino implementations.
type Config struct {
	InterfaceRegistry codectypes.InterfaceRegistry
	Codec             codec.Codec
	TxConfig          client.TxConfig
	Amino             *codec.LegacyAmino
}

// MakeConfig creates an encoding config for the app.
func MakeConfig(regs ...ModuleRegister) Config {
	// create the codec
	amino := codec.NewLegacyAmino()
	interfaceRegistry := codectypes.NewInterfaceRegistry()

	// register the standard types from the sdk
	std.RegisterLegacyAminoCodec(amino)
	std.RegisterInterfaces(interfaceRegistry)

	// register specific modules
	for _, reg := range regs {
		reg.RegisterInterfaces(interfaceRegistry)
		reg.RegisterLegacyAminoCodec(amino)
	}

	// create the final configuration
	marshaler := codec.NewProtoCodec(interfaceRegistry)
	txCfg := tx.NewTxConfig(marshaler, tx.DefaultSignModes)

	return Config{
		InterfaceRegistry: interfaceRegistry,
		Codec:             marshaler,
		TxConfig:          txCfg,
		Amino:             amino,
	}
}
