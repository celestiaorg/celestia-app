package keeper

import (
	"context"
	"fmt"
	"sort"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
)

// InitGenesis initialises the module genesis state.
func (k *Keeper) InitGenesis(ctx context.Context, gs *types.GenesisState) error {
	for _, ism := range gs.Isms {
		if err := k.isms.Set(ctx, ism.Id.GetInternalId(), ism); err != nil {
			return err
		}
	}

	for _, genesisMessages := range gs.Messages {
		exists, err := k.isms.Has(ctx, genesisMessages.Id.GetInternalId())
		if err != nil {
			return err
		}

		if !exists {
			return errorsmod.Wrapf(types.ErrIsmNotFound, "messages defined for unknown ism %s", genesisMessages.Id.String())
		}

		for _, message := range genesisMessages.Messages {
			messageId, err := types.DecodeHex(message)
			if err != nil {
				return fmt.Errorf("invalid message id %q: %w", message, err)
			}

			if err := k.messages.Set(ctx, collections.Join(genesisMessages.Id.GetInternalId(), messageId)); err != nil {
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

	messageIndex := make(map[uint64]*types.GenesisMessages, len(isms))
	for _, ism := range isms {
		ismCopy := ism
		messageIndex[ism.Id.GetInternalId()] = &types.GenesisMessages{Id: ismCopy.Id}
	}

	if err := k.messages.Walk(ctx, nil, func(key collections.Pair[uint64, []byte]) (bool, error) {
		genesisMessages, ok := messageIndex[key.K1()]
		if !ok {
			return false, errorsmod.Wrapf(types.ErrIsmNotFound, "messages found for unknown ism internal id %d", key.K1())
		}

		genesisMessages.Messages = append(genesisMessages.Messages, types.EncodeHex(key.K2()))
		return false, nil
	}); err != nil {
		return nil, err
	}

	genesisMessages := make([]types.GenesisMessages, 0, len(messageIndex))
	for _, messages := range messageIndex {
		sort.Strings(messages.Messages)
		if len(messages.Messages) == 0 {
			continue
		}

		genesisMessages = append(genesisMessages, *messages)
	}

	sort.Slice(genesisMessages, func(i, j int) bool {
		return genesisMessages[i].Id.Compare(genesisMessages[j].Id) < 0
	})

	return &types.GenesisState{
		Isms:     isms,
		Messages: genesisMessages,
	}, nil
}
