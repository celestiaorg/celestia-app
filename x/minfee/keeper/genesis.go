package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/celestiaorg/celestia-app/v4/x/minfee/types"
)

// InitGenesis initializes the blob module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	if err := genState.Params.Validate(); err != nil {
		return fmt.Errorf("invalid minfee genesis state parameters: %w", err)
	}

	k.SetParams(sdkCtx, genState.Params)
	return nil
}

// ExportGenesis returns the blob module's exported genesis.
func (k Keeper) ExportGenesis(ctx context.Context) *types.GenesisState {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	genesis := types.DefaultGenesis()
	// TODO: genesis should hold params not this field.
	genesis.NetworkMinGasPrice = k.GetParams(sdkCtx).NetworkMinGasPrice
	return genesis
}
