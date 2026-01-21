package types

const (
	ModuleName = "forwarding"
	StoreKey   = ModuleName

	// MaxTokensPerForward prevents unbounded iteration and gas exhaustion.
	MaxTokensPerForward = 20
)
