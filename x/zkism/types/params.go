package types

const (
	// DefaultMaxHeaderHashes is 50000.
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
