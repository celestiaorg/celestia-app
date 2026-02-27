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
	// ProcessedPaymentsByHashKeyPrefix is the prefix for processed payment promise keys indexed by hash
	ProcessedPaymentsByHashKeyPrefix = []byte{0x05}
	// ProcessedPaymentsByTimeKeyPrefix is the prefix for processed payment keys indexed by processed time
	ProcessedPaymentsByTimeKeyPrefix = []byte{0x06}
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

// WithdrawalsByAvailableKey returns the store key for a withdrawal indexed by available time
// This index is used for efficient time-ordered iteration in BeginBlocker
func WithdrawalsByAvailableKey(availableAt time.Time, signer string) []byte {
	key := WithdrawalsByAvailableKeyPrefix
	timestampBytes := sdk.FormatTimeBytes(availableAt)
	key = append(key, timestampBytes...)
	key = append(key, []byte("/")...)
	return append(key, []byte(signer)...)
}

// WithdrawalsByAvailablePrefix returns the prefix for all withdrawals available up to a certain time
func WithdrawalsByAvailablePrefix(availableAt time.Time) []byte {
	timestampBytes := sdk.FormatTimeBytes(availableAt)
	return append(WithdrawalsByAvailableKeyPrefix, timestampBytes...)
}

// ProcessedPaymentsByHashKey returns the store key for a processed payment indexed by hash.
// Note: all payment promises that are stored in the SDK module state have already been processed.
func ProcessedPaymentsByHashKey(paymentPromiseHash []byte) []byte {
	return append(ProcessedPaymentsByHashKeyPrefix, paymentPromiseHash...)
}

// ProcessedPaymentsByTimeKey returns the store key for a processed payment indexed by processed time
// This index is used for efficient time-ordered iteration in BeginBlocker for pruning
func ProcessedPaymentsByTimeKey(processedAt time.Time, paymentPromiseHash []byte) []byte {
	key := ProcessedPaymentsByTimeKeyPrefix
	timestampBytes := sdk.FormatTimeBytes(processedAt)
	key = append(key, timestampBytes...)
	key = append(key, []byte("/")...)
	return append(key, paymentPromiseHash...)
}

// ProcessedPaymentsByTimePrefix returns the prefix for all processed payments up to a certain time
func ProcessedPaymentsByTimePrefix(processedAt time.Time) []byte {
	timestampBytes := sdk.FormatTimeBytes(processedAt)
	return append(ProcessedPaymentsByTimeKeyPrefix, timestampBytes...)
}
