package types

const (
	ModuleName = "forwarding"

	// MaxTokensPerForward prevents unbounded iteration and gas exhaustion.
	MaxTokensPerForward = 20
)
