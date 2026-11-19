package keeper

import (
	"context"
	"fmt"
	"sort"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v11/x/teeism/types"
)

// InitGenesis initialises the module genesis state.
func (k *Keeper) InitGenesis(ctx context.Context, gs *types.GenesisState) error {
	for _, ism := range gs.Isms {
		if err := k.isms.Set(ctx, ism.Id.GetInternalId(), ism); err != nil {
			return err
		}
	}

	// Each entry lists the ism's window oldest first, so the last root is number
	// root_seq - 1 (GenesisState.Validate checks the window is complete).
	for _, entry := range gs.Roots {
		ismId := entry.Id
		ism, err := k.isms.Get(ctx, ismId.GetInternalId())
		if err != nil {
			return errorsmod.Wrapf(types.ErrIsmNotFound, "roots defined for unknown ism %s", ismId.String())
		}
		if uint64(len(entry.Roots)) > ism.RootSeq {
			return errorsmod.Wrapf(types.ErrInvalidTrustedState, "ism %s lists %d roots but has attested %d", ismId.String(), len(entry.Roots), ism.RootSeq)
		}

		first := ism.RootSeq - uint64(len(entry.Roots))
		for i, root := range entry.Roots {
			raw, err := types.DecodeHex(root)
			if err != nil {
				return fmt.Errorf("invalid tree root %q: %w", root, err)
			}
			if err := k.roots.Set(ctx, collections.Join(ismId.GetInternalId(), raw)); err != nil {
				return err
			}
			if err := k.rootOrder.Set(ctx, collections.Join(ismId.GetInternalId(), first+uint64(i)), raw); err != nil {
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
	for _, ism := range isms {
		// In attestation order, oldest first, as InitGenesis numbers them.
		var roots []string
		rng := collections.NewPrefixedPairRange[uint64, uint64](ism.Id.GetInternalId())
		if err := k.rootOrder.Walk(ctx, rng, func(_ collections.Pair[uint64, uint64], root []byte) (bool, error) {
			roots = append(roots, types.EncodeHex(root))
			return false, nil
		}); err != nil {
			return nil, errorsmod.Wrapf(err, "collecting roots for ism %s", ism.Id.String())
		}
		if len(roots) == 0 {
			continue
		}

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
