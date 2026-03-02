package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers the necessary x/fibre interfaces and concrete types
// on the provided LegacyAmino codec. These types are used for Amino JSON serialization.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgDepositToEscrow{}, "fibre/MsgDepositToEscrow", nil)
	cdc.RegisterConcrete(&MsgRequestWithdrawal{}, "fibre/MsgRequestWithdrawal", nil)
	cdc.RegisterConcrete(&MsgPayForFibre{}, "fibre/MsgPayForFibre", nil)
	cdc.RegisterConcrete(&MsgPaymentPromiseTimeout{}, "fibre/MsgPaymentPromiseTimeout", nil)
	cdc.RegisterConcrete(&MsgUpdateFibreParams{}, "fibre/MsgUpdateFibreParams", nil)
}

// RegisterInterfaces registers the x/fibre interfaces types with the interface registry
func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgDepositToEscrow{},
		&MsgRequestWithdrawal{},
		&MsgPayForFibre{},
		&MsgPaymentPromiseTimeout{},
		&MsgUpdateFibreParams{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &Msg_serviceDesc)
}

var ModuleCdc = codec.NewProtoCodec(cdctypes.NewInterfaceRegistry())
