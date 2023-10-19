package types

import (
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// ModuleName is the name of the module.
	ModuleName = "qgb"

	// StoreKey to be used when creating the KVStore.
	StoreKey = ModuleName

	// RouterKey is the module name router key.
	RouterKey = ModuleName

	// QuerierRoute to be used for querier msgs.
	QuerierRoute = ModuleName

	// MemStoreKey defines the in-memory store key.
	MemStoreKey = "mem_qgb"
)

const (
	// AttestationRequestKey indexes attestation requests by nonce
	AttestationRequestKey = "AttestationRequestKey"

	// LatestUnBondingBlockHeight indexes the latest validator unbonding block
	// height
	LatestUnBondingBlockHeight = "LatestUnBondingBlockHeight"

	// LatestAttestationNonce indexes the latest attestation request nonce
	LatestAttestationNonce = "LatestAttestationNonce"

	// EarliestAvailableAttestationNonce indexes the earliest available
	// attestation nonce
	EarliestAvailableAttestationNonce = "EarliestAvailableAttestationNonce"

	// EVMAddress indexes evm addresses by validator address
	EVMAddress = "EVMAddress"
)

// GetAttestationKey returns the following key format
// prefix    nonce
// [0x0][0 0 0 0 0 0 0 1]
func GetAttestationKey(nonce uint64) string {
	return AttestationRequestKey + string(UInt64Bytes(nonce))
}

func ConvertByteArrToString(value []byte) string {
	var ret strings.Builder
	for i := 0; i < len(value); i++ {
		ret.WriteString(string(value[i]))
	}
	return ret.String()
}

func GetEVMKey(valAddress sdk.ValAddress) []byte {
	return append([]byte(EVMAddress), valAddress...)
}
