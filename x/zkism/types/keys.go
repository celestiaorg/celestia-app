package types

import (
	"encoding/hex"
	"fmt"

	"cosmossdk.io/collections"
)

const (
	// ModuleName defines the module name
	ModuleName = "zkism"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName
)

var (
	IsmsKeyPrefix    = collections.NewPrefix(0)
	MessageKeyPrefix = collections.NewPrefix(1)
)

// EncodeHex is a convenience function to encode byte slices as 0x prefixed hexadecimal strings.
func EncodeHex(bz []byte) string {
	return fmt.Sprintf("0x%s", hex.EncodeToString(bz))
}
