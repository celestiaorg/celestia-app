package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers the module's Amino interfaces.
func RegisterLegacyAminoCodec(_ *codec.LegacyAmino) {}

// RegisterInterfaces registers the module's interface types.
func RegisterInterfaces(cdc cdctypes.InterfaceRegistry) {
	cdc.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreateInterchainAccountsRouter{},
		&MsgEnrollRemoteRouter{},
		&MsgWarpForward{},
	)

	msgservice.RegisterMsgServiceDesc(cdc, &_Msg_serviceDesc)
}
