package types

import "cosmossdk.io/collections"

const (
	ModuleName = "forwarding"
	StoreKey   = ModuleName
	ParamsKey  = "params"

	// MaxTokensPerForward prevents unbounded iteration and gas exhaustion.
	MaxTokensPerForward = 20

	EventTypeTokenForwarded     = "forwarding.token_forwarded"
	EventTypeForwardingComplete = "forwarding.forwarding_complete"
	EventTypeTokensStuck        = "forwarding.tokens_stuck"
)

var ParamsPrefix = collections.NewPrefix(1)
