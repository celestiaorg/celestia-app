package types

import (
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterInterfaces registers the module msgs against interface types.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgCreateEvolveEvmISM{},
		&MsgUpdateEvolveEvmISM{},
		&MsgSubmitMessages{},
		&MsgUpdateParams{},
	)

	registry.RegisterImplementations(
		(*ismtypes.HyperlaneInterchainSecurityModule)(nil),
		&EvolveEvmISM{},
		&ConsensusISM{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
