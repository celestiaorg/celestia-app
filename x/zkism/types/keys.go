package types

import (
	"encoding/hex"
	"fmt"
	"strings"

	"cosmossdk.io/collections"
)

const (
	// ModuleName defines the module name
	ModuleName = "zkism"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// MaxPaginationLimit is the maximum number of items returned in a paginated query.
	MaxPaginationLimit = 100

	// MaxPublicValuesBytes is the maximum size permitted for public values payloads (4 KiB = 4096 bytes).
	MaxPublicValuesBytes = 4096

	// MinStateBytes is the minimumm size of the trusted ism state (32 bytes).
	MinStateBytes = 32

	// MaxStateBytes is the maximum size of the trusted ism state (2 KiB = 2048 bytes).
	MaxStateBytes = 2048

	// DefaultProofVerifyCostGroth16 is the default gas cost metered for verifying a groth16 proof.
	// NOTE: This is informed by benchmark comparisons with Secp256k1 signature verification.
	// See internal/groth16/bench_test.go
	DefaultProofVerifyCostGroth16 = 6000
)

var (
	IsmsKeyPrefix               = collections.NewPrefix(0)
	MessageKeyPrefix            = collections.NewPrefix(1)
	MessageProofSubmittedPrefix = collections.NewPrefix(2)
)

// EncodeHex is a convenience function to encode byte slices as 0x prefixed hexadecimal strings.
func EncodeHex(bz []byte) string {
	return fmt.Sprintf("0x%s", hex.EncodeToString(bz))
}

// DecodeHex is a convenience function to decode 0x prefixed hexadecimal strings as byte slices.
func DecodeHex(s string) ([]byte, error) {
	s = strings.TrimPrefix(s, "0x")

	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, err
	}

	return b, nil
}
