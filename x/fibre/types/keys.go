package types

const (
	// ModuleName defines the module name
	ModuleName = "fibre"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// MemStoreKey defines the in-memory store key
	MemStoreKey = "mem_fibre"

	// FibreProviderInfoKey defines the key prefix for fibre provider info
	FibreProviderInfoKey = "fibre_provider_info/"
)

func KeyPrefix(p string) []byte {
	return []byte(p)
}

// FibreProviderInfoStoreKey returns the store key for a given validator address
func FibreProviderInfoStoreKey(validatorAddr string) []byte {
	return []byte(FibreProviderInfoKey + validatorAddr)
}