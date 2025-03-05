package codec

import (
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/std"
	"github.com/cosmos/cosmos-sdk/x/auth/tx"
)

// ModuleRegistrar defines an interface for modules to register their types
type ModuleRegistrar interface {
	RegisterInterfaces(codectypes.InterfaceRegistry)
}

// MakeVersionedConfig returns an encoding config with a versioned interface registry
// that conditionally applies recursion limits based on the app version.
func MakeVersionedConfig(appVersion uint64, moduleRegisters ...ModuleRegistrar) client.TxConfig {
	// Create the standard interface registry
	interfaceRegistry := codectypes.NewInterfaceRegistry()

	// Register the standard types
	std.RegisterInterfaces(interfaceRegistry)

	// Register module types
	for _, moduleRegister := range moduleRegisters {
		moduleRegister.RegisterInterfaces(interfaceRegistry)
	}

	// Create the versioned interface registry that conditionally applies recursion limits
	versionedRegistry := NewVersionedInterfaceRegistry(interfaceRegistry, appVersion)

	// Create codec using the versioned registry
	protoCodec := codec.NewProtoCodec(versionedRegistry)

	// Create and return a TxConfig using the versioned codec
	return tx.NewTxConfig(protoCodec, tx.DefaultSignModes)
}
