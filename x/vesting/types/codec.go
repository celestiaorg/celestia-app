package types

import (
	"github.com/celestiaorg/celestia-app/x/vesting/exported"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	"github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/gogoproto/proto"
)

// RegisterLegacyAminoCodec registers the vesting interfaces and concrete types on the
// provided LegacyAmino codec. These types are used for Amino JSON serialization
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterInterface((*exported.VestingAccount)(nil), nil)
	cdc.RegisterConcrete(&BaseVestingAccount{}, "celestia/BaseVestingAccount", nil)
	cdc.RegisterConcrete(&ContinuousVestingAccount{}, "celestia/ContinuousVestingAccount", nil)
	cdc.RegisterConcrete(&DelayedVestingAccount{}, "celestia/DelayedVestingAccount", nil)
	cdc.RegisterConcrete(&PeriodicVestingAccount{}, "celestia/PeriodicVestingAccount", nil)
	cdc.RegisterConcrete(&PermanentLockedAccount{}, "celestia/PermanentLockedAccount", nil)
	legacy.RegisterAminoMsg(cdc, &MsgCreateVestingAccount{}, "celestia/MsgCreateVestingAccount")
	legacy.RegisterAminoMsg(cdc, &MsgCreatePermanentLockedAccount{}, "celestia/MsgCreatePermLockedAccount")
}

// RegisterInterface associates protoName with AccountI and VestingAccount
// Interfaces and creates a registry of it's concrete implementations
func RegisterInterfaces(registry types.InterfaceRegistry) {

	var (
		continousVestingAccount = &ContinuousVestingAccount{}
		delayedVestingAccount   = &DelayedVestingAccount{}
		periodicVestingAccount  = &PeriodicVestingAccount{}
		permanentLockedAccount  = &PermanentLockedAccount{}
	)

	proto.RegisterType(continousVestingAccount, continousVestingAccount.String())
	proto.RegisterType(delayedVestingAccount, delayedVestingAccount.String())
	proto.RegisterType(periodicVestingAccount, periodicVestingAccount.String())
	proto.RegisterType(permanentLockedAccount, permanentLockedAccount.String())

	registry.RegisterInterface(
		"celestia.vesting.v1beta1.VestingAccount",
		(*exported.VestingAccount)(nil),
		continousVestingAccount,
		delayedVestingAccount,
		periodicVestingAccount,
		permanentLockedAccount,
	)

	registry.RegisterImplementations(
		(*authtypes.AccountI)(nil),
		&BaseVestingAccount{},
		&DelayedVestingAccount{},
		&ContinuousVestingAccount{},
		&PeriodicVestingAccount{},
		&PermanentLockedAccount{},
	)

	registry.RegisterImplementations(
		(*authtypes.GenesisAccount)(nil),
		&BaseVestingAccount{},
		&DelayedVestingAccount{},
		&ContinuousVestingAccount{},
		&PeriodicVestingAccount{},
		&PermanentLockedAccount{},
	)

	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgCreateVestingAccount{},
		&MsgCreatePermanentLockedAccount{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

var (
	amino     = codec.NewLegacyAmino()
	ModuleCdc = codec.NewAminoCodec(amino)
)
