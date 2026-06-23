package types

import (
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// ModuleName defines the module name
	ModuleName = "valaddr"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// RouterKey defines the module's message routing key
	RouterKey = ModuleName
)

// JailedGracePeriod is how long a validator may remain jailed and unbonded
// before the EndBlocker sweep garbage-collects its Info entry. A
// validator that recovers before this elapses keeps its
// registration. This is a cleanup threshold, not a consensus-critical value, so
// the exact duration is intentionally approximate (~1 month).
const JailedGracePeriod = 30 * 24 * time.Hour

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
