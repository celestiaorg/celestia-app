package appconsts

const (
	// ArabicaChainID is the chain ID for the Arabica testnet.
	ArabicaChainID = "arabica-11"
	// MochaChainID is the chain ID for the Mocha testnet.
	MochaChainID = "mocha-4"
	// MainnetChainID is the chain ID for the Celestia mainnet.
	MainnetChainID = "celestia"
	// TestChainID is the chain ID used for testing.
	TestChainID = "test"
)

// PublicNetworks is a list of chain IDs for public Celestia networks.
var PublicNetworks = []string{ArabicaChainID, MochaChainID, MainnetChainID}
