package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

const URLMsgRegisterEVMAddress = "/celestia.blob.v1.MsgRegisterEVMAddress"

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgRegisterEVMAddress{}, URLMsgRegisterEVMAddress, nil)
}

func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterEVMAddress{},
	)

	registry.RegisterInterface(
		"AttestationRequestI",
		(*AttestationRequestI)(nil),
		&DataCommitment{},
		&Valset{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
