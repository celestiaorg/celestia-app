package minfee

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	params "github.com/cosmos/cosmos-sdk/x/params/keeper"
)

// DefaultGenesis returns the default genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		NetworkMinGasPrice: DefaultNetworkMinGasPrice,
	}
}

// ValidateGenesis performs basic validation of genesis data returning an error for any failed validation criteria.
func ValidateGenesis(genesis *GenesisState) error {
	if genesis.NetworkMinGasPrice.IsNegative() || genesis.NetworkMinGasPrice.IsZero() {
		return fmt.Errorf("network min gas price cannot be negative or zero: %g", genesis.NetworkMinGasPrice)
	}

	return nil
}

// ExportGenesis returns the minfee module's exported genesis.
func ExportGenesis(ctx sdk.Context, k params.Keeper) *GenesisState {
	subspace, exists := k.GetSubspace(ModuleName)
	if !exists {
		panic("minfee subspace not set")
	}
	subspace = RegisterMinFeeParamTable(subspace)

	var networkMinGasPrice sdk.Dec
	subspace.Get(ctx, KeyNetworkMinGasPrice, &networkMinGasPrice)

	return &GenesisState{NetworkMinGasPrice: networkMinGasPrice}
}
