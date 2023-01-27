package types

import (
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

var ModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())

func RegisterCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgPayForBlobs{}, URLMsgPayForBlobs, nil)
}

func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgPayForBlobs{},
	)

	registry.RegisterInterface(
		"cosmos.auth.v1beta1.BaseAccount",
		(*authtypes.AccountI)(nil),
	)

	registry.RegisterImplementations(
		(*authtypes.AccountI)(nil),
		&authtypes.BaseAccount{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

type localEncoder struct{}

func (localEncoder) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	RegisterCodec(cdc)
}

func (localEncoder) RegisterInterfaces(r codectypes.InterfaceRegistry) {
	RegisterInterfaces(r)
}

// makeBlobEncodingConfig uses the blob modules RegisterInterfaces
// function to create an encoding config for the blob module. This is useful
// so that we don't have to import the app package.
func makeBlobEncodingConfig() encoding.Config {
	e := localEncoder{}
	return encoding.MakeConfig(e)
}
