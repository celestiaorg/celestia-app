package keeper

import (
	"context"
	"fmt"
	"sort"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v11/x/teeism/types"
	"github.com/cosmos/cosmos-sdk/types/query"
)

// InitGenesis initialises the module genesis state.
func (k *Keeper) InitGenesis(ctx context.Context, gs *types.GenesisState) error {
	for _, ism := range gs.Isms {
		if err := k.isms.Set(ctx, ism.Id.GetInternalId(), ism); err != nil {
			return err
		}
	}

	for _, entry := range gs.Roots {
		ismId := entry.Id
		exists, err := k.isms.Has(ctx, ismId.GetInternalId())
		if err != nil {
			return err
		}
		if !exists {
			return errorsmod.Wrapf(types.ErrIsmNotFound, "roots defined for unknown ism %s", ismId.String())
		}

		for _, root := range entry.Roots {
			raw, err := types.DecodeHex(root)
			if err != nil {
				return fmt.Errorf("invalid tree root %q: %w", root, err)
			}
			if err := k.roots.Set(ctx, collections.Join(ismId.GetInternalId(), raw)); err != nil {
				return err
			}
		}
	}

	return nil
}

// ExportGenesis outputs the modules state for genesis exports.
func (k *Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	var isms []types.InterchainSecurityModule
	if err := k.isms.Walk(ctx, nil, func(_ uint64, value types.InterchainSecurityModule) (bool, error) {
		isms = append(isms, value)
		return false, nil
	}); err != nil {
		return nil, err
	}

	genesisRoots := make([]types.GenesisRoots, 0, len(isms))
	transform := func(key collections.Pair[uint64, []byte], _ collections.NoValue) (string, error) {
		return types.EncodeHex(key.K2()), nil
	}

	for _, ism := range isms {
		roots, _, err := query.CollectionPaginate(
			ctx,
			k.roots,
			nil,
			transform,
			query.WithCollectionPaginationPairPrefix[uint64, []byte](ism.Id.GetInternalId()),
		)
		if err != nil {
			return nil, errorsmod.Wrapf(err, "collecting roots for ism %s", ism.Id.String())
		}
		if len(roots) == 0 {
			continue
		}

		sort.Strings(roots)
		genesisRoots = append(genesisRoots, types.GenesisRoots{
			Id:    ism.Id,
			Roots: roots,
		})
	}

	sort.Slice(genesisRoots, func(i, j int) bool {
		return genesisRoots[i].Id.Compare(genesisRoots[j].Id) < 0
	})

	return &types.GenesisState{
		Isms:  isms,
		Roots: genesisRoots,
	}, nil
}
