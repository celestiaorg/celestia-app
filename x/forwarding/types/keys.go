package types

import "cosmossdk.io/collections"

const (
	ModuleName = "forwarding"
	StoreKey   = ModuleName
	ParamsKey  = "params"

	// MaxTokensPerForward prevents unbounded iteration and gas exhaustion.
	MaxTokensPerForward = 20
)

var ParamsPrefix = collections.NewPrefix(1)
