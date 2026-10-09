package encoding

import (
	"reflect"

	addresscodec "cosmossdk.io/core/address"
	"cosmossdk.io/x/tx/signing"
	"github.com/celestiaorg/celestia-app/v10/pkg/txutil"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/address"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/std"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkmodule "github.com/cosmos/cosmos-sdk/types/module"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	"github.com/cosmos/gogoproto/proto"
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
	pffSignerContext      *signing.Context
	pffTxConfig           client.TxConfig
	pffTxType             reflect.Type
}

// PFFSignerAddressCodec returns the codec only while the default signer context
// created by MakeConfig is still installed.
func (c Config) PFFSignerAddressCodec(tx sdk.Tx) addresscodec.Codec {
	if c.pffSignerContext == nil || c.TxConfig != c.pffTxConfig || reflect.TypeOf(tx) != c.pffTxType || c.InterfaceRegistry.SigningContext() != c.pffSignerContext {
		return nil
	}
	return c.pffSignerContext.AddressCodec()
}

// MakeConfig returns an encoding config for the app.
func MakeConfig(moduleBasics ...sdkmodule.AppModuleBasic) Config {
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

	for _, mod := range moduleBasics {
		mod.RegisterInterfaces(interfaceRegistry)
		mod.RegisterLegacyAminoCodec(amino)
	}

	protoCodec := codec.NewProtoCodec(interfaceRegistry)
	sdkDecoder := authtx.DefaultTxDecoder(protoCodec)
	wrappedDecoder := blobTxDecoder(indexWrapperDecoder(sdkDecoder))
	txDecoder := rejectEmptyTxDecoder(func(raw []byte) (sdk.Tx, error) {
		if _, plain := txutil.PlainSDKBody(raw); plain {
			return sdkDecoder(raw)
		}
		return wrappedDecoder(raw)
	})

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
		pffSignerContext:      interfaceRegistry.SigningContext(),
		pffTxConfig:           txConfig,
		pffTxType:             reflect.TypeOf(txConfig.NewTxBuilder().GetTx()),
		Codec:                 protoCodec,
		TxConfig:              txConfig,
		Amino:                 amino,
		AddressPrefix:         addressPrefix,
		AddressCodec:          addressCodec,
		ValidatorAddressCodec: validatorAddressCodec,
		ConsensusAddressCodec: consensusAddressCodec,
	}
}
