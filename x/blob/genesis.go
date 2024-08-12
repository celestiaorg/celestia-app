package blob

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v2/x/blob/keeper"
	"github.com/celestiaorg/celestia-app/v2/x/blob/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// InitGenesis initializes the capability module's state from a provided genesis
// state.
func InitGenesis(ctx sdk.Context, k keeper.Keeper, genState types.GenesisState) {
	fmt.Printf("celestia-app v2 InitGenesis x/blob genState.Params %v\n", genState.Params)
	k.SetParams(ctx, genState.Params)
}

// ExportGenesis returns the capability module's exported genesis.
func ExportGenesis(ctx sdk.Context, k keeper.Keeper) *types.GenesisState {
	genesis := types.DefaultGenesis()
	genesis.Params = k.GetParams(ctx)
	return genesis
}
