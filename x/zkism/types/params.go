package types

const (
	// DefaultHistoricalEntries is 50000. Apps that don't use IBC can ignore this
	// value by not adding the staking module to the application module manager's
	// SetOrderBeginBlockers.
	DefaultHeaderHashRetention uint32 = 50000
)

// NewParams creates a new Params instance.
func NewParams(headerHashRetention uint32) Params {
	return Params{
		HeaderHashRetention: headerHashRetention,
	}
}

// DefaultParams returns a default set of parameters.
func DefaultParams() Params {
	return NewParams(
		DefaultHeaderHashRetention,
	)
}
