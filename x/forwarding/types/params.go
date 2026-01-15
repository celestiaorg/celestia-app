package types

import (
	"cosmossdk.io/math"
)

// DefaultMinForwardAmount is the default minimum forward amount (0 = disabled)
var DefaultMinForwardAmount = math.ZeroInt()

// DefaultParams returns the default parameters for the forwarding module
func DefaultParams() Params {
	return Params{
		MinForwardAmount: DefaultMinForwardAmount,
	}
}

// NewParams creates a new Params instance
func NewParams(minForwardAmount math.Int) Params {
	return Params{
		MinForwardAmount: minForwardAmount,
	}
}

// Validate validates the set of params
func (p Params) Validate() error {
	if p.MinForwardAmount.IsNegative() {
		return ErrBelowMinimum
	}
	return nil
}
