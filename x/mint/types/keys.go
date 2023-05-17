package types

// MintKey is the key to use for the keeper store.
var MintKey = []byte{0x00}

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
