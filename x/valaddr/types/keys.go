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

// UnbondedGracePeriod is how long after its UnbondingTime a validator may
// remain not bonded before the EndBlocker sweep garbage-collects its
// FibreProviderInfo entry. A validator that rebonds before this elapses keeps
// its registration. This is a cleanup threshold, not a consensus-critical
// value, so the exact duration is intentionally approximate. A week is plenty:
// a healthy validator unjails or rebonds within hours to at most a few days.
const UnbondedGracePeriod = 7 * 24 * time.Hour

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
