package types

const (
	// ModuleName is the name of the module
	ModuleName = "gqb"

	// StoreKey to be used when creating the KVStore
	StoreKey = ModuleName

	// RouterKey is the module name router key
	RouterKey = ModuleName

	// QuerierRoute to be used for querierer msgs
	QuerierRoute = ModuleName

	// MemStoreKey defines the in-memory store key
	MemStoreKey = "mem_payment"
)
