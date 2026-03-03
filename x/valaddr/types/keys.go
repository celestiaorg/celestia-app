package types

import sdk "github.com/cosmos/cosmos-sdk/types"

const (
	// ModuleName defines the module name
	ModuleName = "valaddr"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// RouterKey defines the module's message routing key
	RouterKey = ModuleName
)

// Store key prefixes
var (
	// FibreProviderInfoPrefix is the prefix for storing fibre provider info
	// Key format: 0x01 | ConsensusAddress -> ProtocolBuffer(FibreProviderInfo)
	FibreProviderInfoPrefix = []byte{0x01}
)

// GetFibreProviderInfoKey returns the store key for a validator's fibre provider info
func GetFibreProviderInfoKey(consAddr sdk.ConsAddress) []byte {
	return append(FibreProviderInfoPrefix, consAddr.Bytes()...)
}
