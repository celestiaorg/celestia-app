package qgb

import (
	"github.com/celestiaorg/celestia-app/x/qgb/keeper"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// InitGenesis initializes the capability module's state from a provided genesis
// state.
func InitGenesis(ctx sdk.Context, k keeper.Keeper, genState types.GenesisState) {
	k.SetLatestAttestationNonce(ctx, 0)
	// The reason we're setting the earliest available nonce to 1 is because at
	// chain startup, a new valset will always be created. Also, it's easier to
	// set it once here rather than conditionally setting it in abci.EndBlocker
	// which is executed on every block.
	k.SetEarliestAvailableAttestationNonce(ctx, 1)
	k.SetParams(ctx, *genState.Params)
}

// ExportGenesis returns the capability module's exported genesis.
func ExportGenesis(ctx sdk.Context, k keeper.Keeper) *types.GenesisState {
	genesis := types.DefaultGenesis()
	genesis.Params.DataCommitmentWindow = k.GetDataCommitmentWindowParam(ctx)
	return genesis
}
