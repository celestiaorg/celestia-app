package types

import (
	"cosmossdk.io/math"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
)

// DefaultMinForwardAmount is the default minimum forward amount (0 = disabled)
var DefaultMinForwardAmount = math.ZeroInt()

// DefaultTiaCollateralTokenId is the default TIA collateral token ID (placeholder - must be configured)
var DefaultTiaCollateralTokenId = ""

// DefaultParams returns the default parameters for the forwarding module
func DefaultParams() Params {
	return Params{
		MinForwardAmount:     DefaultMinForwardAmount,
		TiaCollateralTokenId: DefaultTiaCollateralTokenId,
	}
}

// NewParams creates a new Params instance
func NewParams(minForwardAmount math.Int, tiaCollateralTokenId string) Params {
	return Params{
		MinForwardAmount:     minForwardAmount,
		TiaCollateralTokenId: tiaCollateralTokenId,
	}
}

// Validate validates the set of params
func (p Params) Validate() error {
	if p.MinForwardAmount.IsNegative() {
		return ErrBelowMinimum
	}

	// TiaCollateralTokenId can be empty (disabled) or must be a valid hex address
	if p.TiaCollateralTokenId != "" {
		_, err := util.DecodeHexAddress(p.TiaCollateralTokenId)
		if err != nil {
			return ErrUnsupportedToken
		}
	}

	return nil
}
