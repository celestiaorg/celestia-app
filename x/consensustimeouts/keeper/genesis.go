package keeper

import (
	"github.com/celestiaorg/celestia-app/v9/x/consensustimeouts/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// InitGenesis writes the GenesisState's Params into the module store.
func (k Keeper) InitGenesis(ctx sdk.Context, g types.GenesisState) {
	k.SetParams(ctx, g.Params)
}

// ExportGenesis returns the current module state as a GenesisState.
func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	return types.NewGenesisState(k.GetParams(ctx))
}
