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
	// The reason we're starting the last available nonce at 1 is because at chain startup,
	// a new valset will be created all the time.
	// Also, it's easier to set it here to 1 instead of doing it in abci.EndBlocker and do
	// the check on every iteration
	k.SetLastAvailableAttestationNonce(ctx, 1)
	k.SetLastUnbondingNonce(ctx, 0)
	k.SetParams(ctx, *genState.Params)
}

// ExportGenesis returns the capability module's exported genesis.
func ExportGenesis(ctx sdk.Context, k keeper.Keeper) *types.GenesisState {
	genesis := types.DefaultGenesis()
	genesis.Params.DataCommitmentWindow = k.GetDataCommitmentWindowParam(ctx)
	return genesis
}
