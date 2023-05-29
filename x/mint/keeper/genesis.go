package keeper

import (
	"github.com/celestiaorg/celestia-app/x/mint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// InitGenesis new mint genesis
func (keeper Keeper) InitGenesis(ctx sdk.Context, ak types.AccountKeeper, data *types.GenesisState) {
	keeper.SetMinter(ctx, data.Minter)
}

// ExportGenesis returns a mint GenesisState for the given context.
func (keeper Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	minter := keeper.GetMinter(ctx)
	return types.NewGenesisState(minter)
}
