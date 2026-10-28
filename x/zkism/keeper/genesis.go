package keeper

import (
	"context"
	"sort"

	errorsmod "cosmossdk.io/errors"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v11/x/zkism/types"
)

// InitGenesis initialises the module genesis state.
func (k *Keeper) InitGenesis(ctx context.Context, gs *types.GenesisState) error {
	for _, ism := range gs.Isms {
		if err := k.isms.Set(ctx, ism.Id.GetInternalId(), ism); err != nil {
			return err
		}
	}

	for _, entry := range gs.TreeStates {
		ismId := entry.Id
		exists, err := k.isms.Has(ctx, ismId.GetInternalId())
		if err != nil {
			return err
		}
		if !exists {
			return errorsmod.Wrapf(types.ErrIsmNotFound, "merkle tree state defined for unknown ism %s", ismId.String())
		}

		if err := k.treeStates.Set(ctx, ismId.GetInternalId(), entry.TreeState); err != nil {
			return err
		}
	}

	for _, entry := range gs.Submissions {
		ismId := entry.Id
		exists, err := k.isms.Has(ctx, ismId.GetInternalId())
		if err != nil {
			return err
		}
		if !exists {
			return errorsmod.Wrapf(types.ErrIsmNotFound, "message proof submitted entry for unknown ism %s", ismId.String())
		}

		if err := k.submissions.Set(ctx, ismId.GetInternalId(), entry.Submitted); err != nil {
			return err
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

	ismByID := make(map[uint64]util.HexAddress, len(isms))
	for i := range isms {
		ism := isms[i]
		ismByID[ism.Id.GetInternalId()] = ism.Id
	}

	submissions := make([]types.GenesisProofSubmission, 0)
	if err := k.submissions.Walk(ctx, nil, func(key uint64, value bool) (bool, error) {
		ismId, ok := ismByID[key]
		if !ok {
			return false, errorsmod.Wrapf(types.ErrIsmNotFound, "message proof submitted entry for unknown ism %d", key)
		}

		submissions = append(submissions, types.GenesisProofSubmission{
			Id:        ismId,
			Submitted: value,
		})
		return false, nil
	}); err != nil {
		return nil, err
	}

	sort.Slice(submissions, func(i, j int) bool {
		return submissions[i].Id.Compare(submissions[j].Id) < 0
	})

	treeStates := make([]types.GenesisMerkleTreeState, 0)
	if err := k.treeStates.Walk(ctx, nil, func(key uint64, value types.MerkleTreeState) (bool, error) {
		ismId, ok := ismByID[key]
		if !ok {
			return false, errorsmod.Wrapf(types.ErrIsmNotFound, "merkle tree state entry for unknown ism %d", key)
		}

		treeStates = append(treeStates, types.GenesisMerkleTreeState{
			Id:        ismId,
			TreeState: value,
		})
		return false, nil
	}); err != nil {
		return nil, err
	}

	sort.Slice(treeStates, func(i, j int) bool {
		return treeStates[i].Id.Compare(treeStates[j].Id) < 0
	})

	return &types.GenesisState{
		Isms:        isms,
		Submissions: submissions,
		TreeStates:  treeStates,
	}, nil
}
