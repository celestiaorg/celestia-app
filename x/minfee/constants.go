package minfee

import sdk "github.com/cosmos/cosmos-sdk/types"

const (
	// DefaultNetworkMinGasPrice is used by x/minfee to prevent transactions from being
	// included in a block if they specify a gas price lower than this.
	// Only applies to app version >= 2
	DefaultNetworkMinGasPrice = 0.000001 // utia
)

var (
	defaultNetworkMinGasPriceAsDec = sdk.NewDecWithPrec(1, 6)
)

func DefaultNetworkMinGasPriceAsDec() sdk.Dec {
	return defaultNetworkMinGasPriceAsDec
}
