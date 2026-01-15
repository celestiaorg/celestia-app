package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
)

func RegisterInterfaces(_ codectypes.InterfaceRegistry) {
	// No messages to register - feeaddress module uses fee address approach
}

func RegisterLegacyAminoCodec(_ *codec.LegacyAmino) {
	// No messages to register - feeaddress module uses fee address approach
}
