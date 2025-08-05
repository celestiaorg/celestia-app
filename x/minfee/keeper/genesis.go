package keeper

import (
	"context"
	"fmt"

	"github.com/celestiaorg/celestia-app/v6/x/minfee/types"
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
	genesis := types.DefaultGenesis()
	// TODO: genesis should hold params not this field.
	genesis.NetworkMinGasPrice = k.GetParams(sdkCtx).NetworkMinGasPrice
	return genesis
}
