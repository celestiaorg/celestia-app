package types

const (
	// DefaultHistoricalEntries is 50000. Apps that don't use IBC can ignore this
	// value by not adding the staking module to the application module manager's
	// SetOrderBeginBlockers.
	DefaultMaxHeaderHashes uint32 = 50000
)

// NewParams creates a new Params instance.
func NewParams(maxHeaderHashes uint32) Params {
	return Params{
		MaxHeaderHashes: maxHeaderHashes,
	}
}

// DefaultParams returns a default set of parameters.
func DefaultParams() Params {
	return NewParams(
		DefaultMaxHeaderHashes,
	)
}
