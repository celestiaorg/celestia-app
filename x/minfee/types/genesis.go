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
	if genesis.NetworkMinGasPrice.IsNegative() || genesis.NetworkMinGasPrice.IsZero() {
		return fmt.Errorf("network min gas price cannot be negative or zero: %g", genesis.NetworkMinGasPrice)
	}

	return genesis.Params.Validate()
}
