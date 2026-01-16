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
)

var (
	IsmsKeyPrefix    = collections.NewPrefix(0)
	MessageKeyPrefix = collections.NewPrefix(1)
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
