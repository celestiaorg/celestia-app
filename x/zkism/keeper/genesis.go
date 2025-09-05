package keeper

import (
	"context"

	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
)

// InitGenesis initialises the module genesis state.
func (k *Keeper) InitGenesis(ctx context.Context, gs *types.GenesisState) error {
	for _, ism := range gs.Isms {
		if err := k.isms.Set(ctx, ism.Id.GetInternalId(), ism); err != nil {
			return err
		}
	}

	return k.params.Set(ctx, gs.Params)
}

// ExportGenesis outputs the modules state for genesis exports.
func (k *Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	var isms []types.ZKExecutionISM
	if err := k.isms.Walk(ctx, nil, func(_ uint64, value types.ZKExecutionISM) (bool, error) {
		isms = append(isms, value)
		return false, nil
	}); err != nil {
		return nil, err
	}

	params, err := k.params.Get(ctx)
	if err != nil {
		return nil, err
	}

	return &types.GenesisState{
		Isms:   isms,
		Params: params,
	}, nil
}
