package types

import (
	"fmt"

	"cosmossdk.io/math"
)

// DefaultMinForwardAmount is the default minimum forward amount (0 = no minimum)
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
		return fmt.Errorf("min_forward_amount cannot be negative: %s", p.MinForwardAmount)
	}
	return nil
}
