package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v7/x/zkism/types"
)

// HasMessageId is a test func used for checking existence of a message id in the messages store collection.
func (k *Keeper) HasMessageId(ctx context.Context, ismId util.HexAddress, messageId []byte) (bool, error) {
	return k.messages.Has(ctx, collections.Join(ismId.GetInternalId(), messageId))
}

// SetMessageId is a test func used for setting a message id in the messages store collection.
func (k *Keeper) SetMessageId(ctx context.Context, ismId util.HexAddress, messageId []byte) error {
	return k.messages.Set(ctx, collections.Join(ismId.GetInternalId(), messageId))
}

// GetIsm is a test func used for getting an ISM in the isms store collection.
func (k *Keeper) GetIsm(ctx context.Context, ismId util.HexAddress) (types.InterchainSecurityModule, error) {
	return k.isms.Get(ctx, ismId.GetInternalId())
}

// SetIsm is a test func used for setting an ISM in the isms store collection.
func (k *Keeper) SetIsm(ctx context.Context, ismId util.HexAddress, ism types.InterchainSecurityModule) error {
	return k.isms.Set(ctx, ismId.GetInternalId(), ism)
}
