package types

// MinterKey is the key to use for the keeper store.
var MinterKey = []byte{0x00}

// GenesisTimeKey is the key to use for the genesis time in the keeper store.
var GenesisTimeKey = []byte{0x01}

const (
	// ModuleName is the name of the mint module.
	ModuleName = "mint"

	// StoreKey is the default store key for mint
	StoreKey = ModuleName

	// QuerierRoute is the querier route for the mint store.
	QuerierRoute = StoreKey

	// Query endpoints supported by the mint querier
	QueryParameters       = "parameters"
	QueryInflationRate    = "inflation_rate"
	QueryAnnualProvisions = "annual_provisions"
	QueryGenesisTime      = "genesis_time"
)
