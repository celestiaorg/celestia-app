package types

const (
	// ModuleName defines the module name.
	ModuleName = "ethidentity"

	// StoreKey defines the primary module store key.
	StoreKey = ModuleName
)

var (
	// EthAddressIndexPrefix prefixes Ethereum address to Celestia address index
	// entries.
	EthAddressIndexPrefix = []byte{0x01}
)
