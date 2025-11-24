package keeper

import (
	"context"

	hyperlanetypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
)

// InitGenesis initialises the module genesis state.
func (k *Keeper) InitGenesis(ctx context.Context, gs *types.GenesisState) error {
	for _, ism := range gs.Isms {
		if err := k.isms.Set(ctx, ism.Id.GetInternalId(), &ism); err != nil {
			return err
		}
	}

	return k.params.Set(ctx, gs.Params)
}

// ExportGenesis outputs the modules state for genesis exports.
func (k *Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	var isms []types.EvolveEvmISM
	if err := k.isms.Walk(ctx, nil, func(_ uint64, value hyperlanetypes.HyperlaneInterchainSecurityModule) (bool, error) {
		if ism, ok := value.(*types.EvolveEvmISM); ok {
			isms = append(isms, *ism)
		}
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
