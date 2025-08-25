package keeper

import (
	"context"

	"github.com/celestiaorg/celestia-app/v6/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// InitGenesis initializes the fibre module's state from a provided genesis
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) {
	for _, provider := range genState.Providers {
		validatorAddr, err := sdk.ValAddressFromBech32(provider.ValidatorAddress)
		if err != nil {
			panic(err)
		}
		
		k.SetFibreProviderInfo(ctx, validatorAddr, provider.Info)
	}
}

// ExportGenesis returns the fibre module's exported genesis
func (k Keeper) ExportGenesis(ctx context.Context) *types.GenesisState {
	var providers []types.GenesisProvider
	
	k.IterateAllFibreProviderInfo(ctx, func(validatorAddr string, info types.FibreProviderInfo) bool {
		providers = append(providers, types.GenesisProvider{
			ValidatorAddress: validatorAddr,
			Info:             info,
		})
		return false
	})
	
	return types.NewGenesisState(providers)
}