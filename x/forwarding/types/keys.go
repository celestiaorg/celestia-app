package types

const (
	// ModuleName defines the module name
	ModuleName = "forwarding"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// ParamsKey defines the key used for storing module parameters
	ParamsKey = "params"

	// MaxTokensPerForward is the maximum number of tokens that can be forwarded in a single call.
	// This prevents unbounded iteration and gas exhaustion attacks.
	MaxTokensPerForward = 20
)

// Key prefixes for collections
var (
	ParamsPrefix = []byte{0x01}
)
