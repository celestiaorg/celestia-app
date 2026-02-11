package keeper

import (
	"context"
	"fmt"
	"sort"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v7/x/zkism/types"
	"github.com/cosmos/cosmos-sdk/types/query"
)

// InitGenesis initialises the module genesis state.
func (k *Keeper) InitGenesis(ctx context.Context, gs *types.GenesisState) error {
	for _, ism := range gs.Isms {
		if err := k.isms.Set(ctx, ism.Id.GetInternalId(), ism); err != nil {
			return err
		}
	}

	for _, messages := range gs.Messages {
		ismId := messages.Id
		exists, err := k.isms.Has(ctx, ismId.GetInternalId())
		if err != nil {
			return err
		}

		if !exists {
			return errorsmod.Wrapf(types.ErrIsmNotFound, "messages defined for unknown ism %s", ismId.String())
		}

		for _, msg := range messages.Messages {
			messageId, err := types.DecodeHex(msg)
			if err != nil {
				return fmt.Errorf("invalid message id %q: %w", msg, err)
			}

			if err := k.messages.Set(ctx, collections.Join(ismId.GetInternalId(), messageId)); err != nil {
				return err
			}
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

	genesisMessages := make([]types.GenesisMessages, 0, len(isms))
	transform := func(key collections.Pair[uint64, []byte], _ collections.NoValue) (string, error) {
		return types.EncodeHex(key.K2()), nil
	}

	for _, ism := range isms {
		msgs, _, err := query.CollectionPaginate(
			ctx,
			k.messages,
			nil,
			transform,
			query.WithCollectionPaginationPairPrefix[uint64, []byte](ism.Id.GetInternalId()),
		)
		if err != nil {
			return nil, errorsmod.Wrapf(err, "collecting messages for ism %s", ism.Id.String())
		}

		if len(msgs) == 0 {
			continue
		}

		sort.Strings(msgs)
		genesisMessages = append(genesisMessages, types.GenesisMessages{
			Id:       ism.Id,
			Messages: msgs,
		})
	}

	sort.Slice(genesisMessages, func(i, j int) bool {
		return genesisMessages[i].Id.Compare(genesisMessages[j].Id) < 0
	})

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

	return &types.GenesisState{
		Isms:        isms,
		Messages:    genesisMessages,
		Submissions: submissions,
	}, nil
}
