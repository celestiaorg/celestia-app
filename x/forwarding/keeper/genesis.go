package keeper

import (
	"context"

	"github.com/celestiaorg/celestia-app/v7/x/forwarding/types"
)

// InitGenesis initializes the module's state from a genesis state
func (k Keeper) InitGenesis(ctx context.Context, genState *types.GenesisState) error {
	return k.SetParams(ctx, genState.Params)
}

// ExportGenesis exports the module's state to a genesis state
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}

	return &types.GenesisState{
		Params: params,
	}, nil
}
