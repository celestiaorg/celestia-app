package keeper

import (
	"context"
	"fmt"

	"github.com/celestiaorg/celestia-app/v6/x/blob/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// InitGenesis initializes the blob module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := genState.Params.Validate(); err != nil {
		return fmt.Errorf("invalid blob genesis state parameters: %w", err)
	}
	k.SetParams(sdkCtx, genState.Params)
	return nil
}

// ExportGenesis returns the blob module's exported genesis.
func (k Keeper) ExportGenesis(ctx context.Context) *types.GenesisState {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	genesis := types.DefaultGenesis()
	genesis.Params = k.GetParams(sdkCtx)
	return genesis
}
