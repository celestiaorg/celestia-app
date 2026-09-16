package types

import (
	"encoding/hex"
	"fmt"
	"strings"

	"cosmossdk.io/collections"
)

const (
	// ModuleName defines the module name.
	ModuleName = "teeism"

	// StoreKey defines the primary module store key.
	StoreKey = ModuleName

	// MaxPaginationLimit is the maximum number of items returned in a paginated query.
	MaxPaginationLimit = 100

	// MaxQuoteBytes caps a submitted TDX quote. A real dstack quote is around
	// 5 KiB; the headroom covers longer PCK certificate chains.
	MaxQuoteBytes = 32 * 1024

	// MaxEventLogBytes caps the dstack event log.
	MaxEventLogBytes = 256 * 1024

	// MaxCollateralFieldBytes caps any single Intel PCS artifact.
	MaxCollateralFieldBytes = 256 * 1024

	// MaxPayloadBytes caps an attested update. The message ids dominate, so this
	// is the real bound on how many messages one attestation may carry.
	MaxPayloadBytes = 4 * 1024 * 1024

	// DefaultAttestationVerifyCost is the gas metered for one DCAP verification.
	//
	// Verification is a handful of ECDSA P-256 checks over a certificate chain
	// plus JSON and CRL parsing. Measured at roughly 3ms, which puts it in the
	// same range as the groth16 verification x/zkism meters at 6000.
	DefaultAttestationVerifyCost = 6000

	// DefaultMessageIDCost is metered per authorized message id, so a large batch
	// pays for the storage it creates.
	DefaultMessageIDCost = 1000
)

var (
	IsmsKeyPrefix    = collections.NewPrefix(0)
	MessageKeyPrefix = collections.NewPrefix(1)
)

// EncodeHex encodes a byte slice as a 0x prefixed hexadecimal string.
func EncodeHex(bz []byte) string {
	return fmt.Sprintf("0x%s", hex.EncodeToString(bz))
}

// DecodeHex decodes a 0x prefixed hexadecimal string.
func DecodeHex(s string) ([]byte, error) {
	return hex.DecodeString(strings.TrimPrefix(s, "0x"))
}
