package types

import "cosmossdk.io/collections"

const (
	// ModuleName defines the module name
	ModuleName = "zkism"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName
)

var IsmsKeyPrefix = collections.NewPrefix(0)
