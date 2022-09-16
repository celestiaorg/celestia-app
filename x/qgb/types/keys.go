package types

import (
	"strings"
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
	// ValsetRequestKey indexes valset requests by nonce
	AttestationRequestKey = "AttestationRequestKey"

	// LastUnBondingBlockHeight indexes the last validator unbonding block height
	LastUnBondingBlockHeight = "LastUnBondingBlockHeight"

	// LatestAttestationtNonce indexes the latest attestation request nonce
	LatestAttestationtNonce = "LatestAttestationNonce"
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
