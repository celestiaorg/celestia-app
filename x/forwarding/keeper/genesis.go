package keeper

import (
	"context"

	"github.com/celestiaorg/celestia-app/v7/x/forwarding/types"
)

// InitGenesis initializes the module's state from a genesis state.
// The forwarding module is stateless - this is a no-op.
func (k Keeper) InitGenesis(_ context.Context, _ *types.GenesisState) error {
	return nil
}

// ExportGenesis exports the module's state to a genesis state.
// The forwarding module is stateless - returns empty state.
func (k Keeper) ExportGenesis(_ context.Context) (*types.GenesisState, error) {
	return &types.GenesisState{}, nil
}
