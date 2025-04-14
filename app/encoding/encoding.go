package encoding

import (
	addresscodec "cosmossdk.io/core/address"
	"cosmossdk.io/x/tx/signing"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/address"
	codectestutil "github.com/cosmos/cosmos-sdk/codec/testutil"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/std"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkmodule "github.com/cosmos/cosmos-sdk/types/module"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	"github.com/cosmos/gogoproto/proto"

	"github.com/celestiaorg/celestia-app/v4/app/params"
)

// Config specifies the concrete encoding types to use for a given app.
// This is provided for compatibility between protobuf and amino implementations.
type Config struct {
	InterfaceRegistry     codectypes.InterfaceRegistry
	Codec                 codec.Codec
	TxConfig              client.TxConfig
	Amino                 *codec.LegacyAmino
	AddressPrefix         string
	AddressCodec          addresscodec.Codec
	ValidatorAddressCodec addresscodec.Codec
	ConsensusAddressCodec addresscodec.Codec
}

// MakeConfig returns an encoding config for the app.
func MakeConfig(...sdkmodule.AppModuleBasic) Config {
	addressPrefix, validatorPrefix := sdk.GetConfig().GetBech32AccountAddrPrefix(), sdk.GetConfig().GetBech32ValidatorAddrPrefix()
	addressCodec := address.NewBech32Codec(addressPrefix)
	validatorAddressCodec := address.NewBech32Codec(validatorPrefix)
	consensusAddressCodec := address.NewBech32Codec(sdk.GetConfig().GetBech32ConsensusAddrPrefix())

	interfaceRegistry, _ := codectypes.NewInterfaceRegistryWithOptions(codectypes.InterfaceRegistryOptions{
		ProtoFiles: proto.HybridResolver,
		SigningOptions: signing.Options{
			AddressCodec:          addressCodec,
			ValidatorAddressCodec: validatorAddressCodec,
		},
	})
	amino := codec.NewLegacyAmino()

	// Register the standard types from the Cosmos SDK on interfaceRegistry and amino.
	std.RegisterInterfaces(interfaceRegistry)
	std.RegisterLegacyAminoCodec(amino)

	protoCodec := codec.NewProtoCodec(interfaceRegistry)
	txDecoder := authtx.DefaultTxDecoder(protoCodec)
	txDecoder = indexWrapperDecoder(txDecoder)

	txConfig, err := authtx.NewTxConfigWithOptions(protoCodec, authtx.ConfigOptions{
		EnabledSignModes: authtx.DefaultSignModes,
		SigningOptions: &signing.Options{
			AddressCodec:          addressCodec,
			ValidatorAddressCodec: validatorAddressCodec,
		},
		ProtoDecoder: txDecoder,
	})
	if err != nil {
		panic(err)
	}

	return Config{
		InterfaceRegistry:     interfaceRegistry,
		Codec:                 protoCodec,
		TxConfig:              txConfig,
		Amino:                 amino,
		AddressPrefix:         addressPrefix,
		AddressCodec:          addressCodec,
		ValidatorAddressCodec: validatorAddressCodec,
		ConsensusAddressCodec: consensusAddressCodec,
	}
}

func MakeTestConfig(moduleBasics ...sdkmodule.AppModuleBasic) Config {
	codecOpts := codectestutil.CodecOptions{AccAddressPrefix: params.Bech32PrefixAccAddr, ValAddressPrefix: params.Bech32PrefixValAddr}
	enc := moduletestutil.MakeTestEncodingConfigWithOpts(codecOpts, moduleBasics...)
	addressPrefix, validatorPrefix := sdk.GetConfig().GetBech32AccountAddrPrefix(), sdk.GetConfig().GetBech32ValidatorAddrPrefix()
	addressCodec := address.NewBech32Codec(addressPrefix)
	validatorAddressCodec := address.NewBech32Codec(validatorPrefix)
	consensusAddressCodec := address.NewBech32Codec(sdk.GetConfig().GetBech32ConsensusAddrPrefix())

	return Config{
		InterfaceRegistry:     enc.InterfaceRegistry,
		Codec:                 enc.Codec,
		TxConfig:              enc.TxConfig,
		Amino:                 enc.Amino,
		AddressPrefix:         addressPrefix,
		AddressCodec:          addressCodec,
		ValidatorAddressCodec: validatorAddressCodec,
		ConsensusAddressCodec: consensusAddressCodec,
	}
}
