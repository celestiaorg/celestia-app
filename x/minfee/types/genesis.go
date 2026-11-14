package types

import (
	"fmt"
)

// DefaultGenesis returns the default genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		NetworkMinGasPrice: DefaultNetworkMinGasPrice, // TODO: remove this field
		Params: Params{
			NetworkMinGasPrice: DefaultNetworkMinGasPrice,
		},
	}
}

// ValidateGenesis performs basic validation of genesis data returning an error for any failed validation criteria.
func ValidateGenesis(genesis *GenesisState) error {
	// NetworkMinGasPrice is a non-nullable LegacyDec, so a genesis that omits
	// the (deprecated) top-level field leaves it as the zero value whose
	// internal big.Int is nil. IsNegative and IsZero dereference that pointer,
	// so guard the nil case before calling them to return an error rather than
	// panicking on a malformed genesis.
	if genesis.NetworkMinGasPrice.IsNil() {
		return fmt.Errorf("network min gas price cannot be nil")
	}
	if genesis.NetworkMinGasPrice.IsNegative() || genesis.NetworkMinGasPrice.IsZero() {
		return fmt.Errorf("network min gas price cannot be negative or zero: %s", genesis.NetworkMinGasPrice)
	}

	return genesis.Params.Validate()
}
