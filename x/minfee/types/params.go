package types

import (
	"fmt"

	"cosmossdk.io/math"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
)

var DefaultNetworkMinGasPrice math.LegacyDec

func init() {
	DefaultNetworkMinGasPriceDec, err := math.LegacyNewDecFromStr(fmt.Sprintf("%f", appconsts.DefaultNetworkMinGasPrice))
	if err != nil {
		panic(err)
	}
	DefaultNetworkMinGasPrice = DefaultNetworkMinGasPriceDec
}

// Validate validates the set of params
func (p Params) Validate() error {
	return nil
}

// DefaultParams returns the default parameters for the module.
func DefaultParams() Params {
	return Params{
		NetworkMinGasPrice: DefaultNetworkMinGasPrice,
	}
}

// NewParams creates a new instance of Params with the provided NetworkMinGasPrice.
func NewParams(networkMinGasPrice math.LegacyDec) Params {
	return Params{
		NetworkMinGasPrice: networkMinGasPrice,
	}
}
