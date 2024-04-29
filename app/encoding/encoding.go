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

// MakeConfig returns an encoding config for the app.
func MakeConfig(moduleRegisters ...ModuleRegister) Config {
	interfaceRegistry := codectypes.NewInterfaceRegistry()
	amino := codec.NewLegacyAmino()

	// Register the standard types from the Cosmos SDK on interfaceRegistry and
	// amino.
	std.RegisterInterfaces(interfaceRegistry)
	std.RegisterLegacyAminoCodec(amino)

	// Register types from the moduleRegisters on interfaceRegistry and amino.
	for _, moduleRegister := range moduleRegisters {
		moduleRegister.RegisterInterfaces(interfaceRegistry)
		moduleRegister.RegisterLegacyAminoCodec(amino)
	}

	protoCodec := codec.NewProtoCodec(interfaceRegistry)
	txConfig := tx.NewTxConfig(protoCodec, tx.DefaultSignModes)
	txDecoder := txConfig.TxDecoder()
	txDecoder = indexWrapperDecoder(txDecoder)
	txConfig.SetTxDecoder(txDecoder)

	return Config{
		InterfaceRegistry: interfaceRegistry,
		Codec:             protoCodec,
		TxConfig:          txConfig,
		Amino:             amino,
	}
}
