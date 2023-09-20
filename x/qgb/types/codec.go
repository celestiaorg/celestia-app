package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gogo/protobuf/proto"
)

const URLMsgRegisterEVMAddress = "/celestia.blob.v1.MsgRegisterEVMAddress"

func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgRegisterEVMAddress{}, URLMsgRegisterEVMAddress, nil)
}

func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterEVMAddress{},
	)

	dataCommitment := &DataCommitment{}
	valSet := &Valset{}
	panic(dataCommitment.String())
	proto.RegisterType(dataCommitment, dataCommitment.String())
	proto.RegisterType(valSet, valSet.String())

	registry.RegisterInterface(
		"AttestationRequestI",
		(*AttestationRequestI)(nil),
		dataCommitment,
		valSet,
	)
}
