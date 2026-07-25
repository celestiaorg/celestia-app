package keeper

import (
	"context"
	"fmt"

	"github.com/celestiaorg/celestia-app/v10/x/minfee/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// InitGenesis initializes the minfee module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	if err := genState.Params.Validate(); err != nil {
		return fmt.Errorf("invalid minfee genesis state parameters: %w", err)
	}

	k.SetParams(sdkCtx, genState.Params)
	return nil
}

// ExportGenesis returns the minfee module's exported genesis.
func (k Keeper) ExportGenesis(ctx context.Context) *types.GenesisState {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params := k.GetParams(sdkCtx)
	return &types.GenesisState{
		Params: params,
		// The deprecated top-level field must stay populated and in sync with
		// params: ValidateGenesis rejects a zero value.
		NetworkMinGasPrice: params.NetworkMinGasPrice,
	}
}
