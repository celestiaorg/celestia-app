package types

import (
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// ModuleName defines the module name
	ModuleName = "fibre"
	// StoreKey defines the primary module store key
	StoreKey = ModuleName
	// RouterKey defines the module's message routing key
	RouterKey = ModuleName
	// ParamsKey defines the key used for storing module parameters
	ParamsKey = "params"
)

// Store key prefixes
var (
	// ParamsKeyPrefix is the prefix for params
	ParamsKeyPrefix = []byte{0x01}
	// EscrowAccountKeyPrefix is the prefix for escrow account keys
	EscrowAccountKeyPrefix = []byte{0x02}
	// WithdrawalsBySignerKeyPrefix is the prefix for withdrawal keys indexed by signer
	WithdrawalsBySignerKeyPrefix = []byte{0x03}
	// WithdrawalsByAvailableKeyPrefix is the prefix for withdrawal keys indexed by available time
	WithdrawalsByAvailableKeyPrefix = []byte{0x04}
	// LegacyProcessedPaymentsByHashKeyPrefix indexes payments processed before v12 by hash.
	LegacyProcessedPaymentsByHashKeyPrefix = []byte{0x05}
	// LegacyProcessedPaymentsByTimeKeyPrefix indexes payments processed before v12 by processed time.
	LegacyProcessedPaymentsByTimeKeyPrefix = []byte{0x06}
	// PromiseFreshnessFloorKey holds a single timestamp: the oldest creation_timestamp we
	// still treat as fresh. It only ever moves forward, which is what stops a settled,
	// already-pruned promise from being replayed after governance raises WithdrawalDelay.
	PromiseFreshnessFloorKey = []byte{0x07}
	// GenesisHeightKey holds the height of the first block this chain's state executed,
	// recorded by InitGenesis. Its signing set has no historical info at the height before.
	GenesisHeightKey = []byte{0x08}
	// ProcessedPaymentsKeyPrefix indexes processed payments by promise creation time, then
	// hash. Settled promises are recent, so inserts and pruning stay at the edges of the tree.
	ProcessedPaymentsKeyPrefix = []byte{0x09}
)

// EscrowAccountKey returns the store key for an escrow account
func EscrowAccountKey(signer string) []byte {
	return append(EscrowAccountKeyPrefix, []byte(signer)...)
}

// WithdrawalsBySignerKey returns the store key for a withdrawal indexed by signer
func WithdrawalsBySignerKey(signer string, requestedTimestamp time.Time) []byte {
	key := WithdrawalsBySignerPrefix(signer)
	key = append(key, []byte("/")...)
	timestampBytes := sdk.FormatTimeBytes(requestedTimestamp)
	return append(key, timestampBytes...)
}

// WithdrawalsBySignerPrefix returns the prefix for all withdrawals by a signer
func WithdrawalsBySignerPrefix(signer string) []byte {
	return append(WithdrawalsBySignerKeyPrefix, []byte(signer)...)
}

// WithdrawalsByAvailableKey returns the store key for a withdrawal indexed by available time.
// Layout: 0x04 || available || "/" || requested || "/" || signer.
// The requested timestamp keeps the key unique per request after a WithdrawalDelay change.
func WithdrawalsByAvailableKey(availableAt, requestedAt time.Time, signer string) []byte {
	key := WithdrawalsByAvailableKeyPrefix
	key = append(key, sdk.FormatTimeBytes(availableAt)...)
	key = append(key, []byte("/")...)
	key = append(key, sdk.FormatTimeBytes(requestedAt)...)
	key = append(key, []byte("/")...)
	return append(key, []byte(signer)...)
}

// WithdrawalsByAvailablePrefix returns the prefix for all withdrawals available up to a certain time
func WithdrawalsByAvailablePrefix(availableAt time.Time) []byte {
	timestampBytes := sdk.FormatTimeBytes(availableAt)
	return append(WithdrawalsByAvailableKeyPrefix, timestampBytes...)
}

// ProcessedPaymentKey returns the store key for a processed payment. The promise hash
// commits to its ns-precise creation time, so a hash maps to exactly one key.
func ProcessedPaymentKey(creationTimestamp time.Time, paymentPromiseHash []byte) []byte {
	return append(ProcessedPaymentsPrefix(creationTimestamp), paymentPromiseHash...)
}

// ProcessedPaymentsPrefix returns the prefix for all processed payments of promises
// created at creationTimestamp.
func ProcessedPaymentsPrefix(creationTimestamp time.Time) []byte {
	return append(ProcessedPaymentsKeyPrefix, sdk.FormatTimeBytes(creationTimestamp)...)
}

// LegacyProcessedPaymentsByHashKey returns the pre-v12 store key for a processed payment indexed by hash.
func LegacyProcessedPaymentsByHashKey(paymentPromiseHash []byte) []byte {
	return append(LegacyProcessedPaymentsByHashKeyPrefix, paymentPromiseHash...)
}

// LegacyProcessedPaymentsByTimeKey returns the pre-v12 store key for a processed payment indexed by processed time.
func LegacyProcessedPaymentsByTimeKey(processedAt time.Time, paymentPromiseHash []byte) []byte {
	key := LegacyProcessedPaymentsByTimeKeyPrefix
	timestampBytes := sdk.FormatTimeBytes(processedAt)
	key = append(key, timestampBytes...)
	key = append(key, []byte("/")...)
	return append(key, paymentPromiseHash...)
}

// LegacyProcessedPaymentsByTimePrefix returns the pre-v12 prefix for all processed payments up to a certain time.
func LegacyProcessedPaymentsByTimePrefix(processedAt time.Time) []byte {
	timestampBytes := sdk.FormatTimeBytes(processedAt)
	return append(LegacyProcessedPaymentsByTimeKeyPrefix, timestampBytes...)
}
