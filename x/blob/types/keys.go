package types

const (
	// ModuleName defines the module name
	ModuleName = "blob"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// MemStoreKey defines the in-memory store key
	MemStoreKey = "mem_blob"

	// ParamsKey defines the key used for storing module parameters
	ParamsKey = "params"
)

func KeyPrefix(p string) []byte {
	return []byte(p)
}
