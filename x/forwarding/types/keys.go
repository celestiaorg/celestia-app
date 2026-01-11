package types

const (
	// ModuleName defines the module name
	ModuleName = "forwarding"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// ParamsKey defines the key used for storing module parameters
	ParamsKey = "params"
)

// Key prefixes for collections
var (
	ParamsPrefix = []byte{0x01}
)
