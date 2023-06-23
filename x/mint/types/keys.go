package types

// KeyMinter is the key to use for the Minter in the mint store.
var KeyMinter = []byte("Minter")

// KeyGenesisTime is the key to use for GenesisTime in the mint store.
var KeyGenesisTime = []byte("GenesisTime")

const (
	// ModuleName is the name of the mint module.
	ModuleName = "mint"

	// StoreKey is the default store key for mint
	StoreKey = ModuleName

	// QuerierRoute is the querier route for the mint store.
	QuerierRoute = StoreKey

	// Query endpoints supported by the mint querier
	QueryInflationRate    = "inflation_rate"
	QueryAnnualProvisions = "annual_provisions"
	QueryGenesisTime      = "genesis_time"
)
