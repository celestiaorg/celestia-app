package types

// DefaultParams returns the default parameters for the forwarding module
func DefaultParams() Params {
	return Params{}
}

// Validate validates the set of params
func (p Params) Validate() error {
	// No validation needed for empty params
	return nil
}
