package keeper

import (
	"context"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
)

// GetIsm is a test func used for checking existence of a message id in the messages store collection.
func (k *Keeper) HasMessageId(ctx context.Context, messageId []byte) (bool, error) {
	return k.messages.Has(ctx, messageId)
}

// SetMessageId is a test func used for setting a message id in the messages store collection.
func (k *Keeper) SetMessageId(ctx context.Context, messageId []byte) error {
	return k.messages.Set(ctx, messageId)
}

// GetIsm is a test func used for getting an ISM in the isms store collection.
func (k *Keeper) GetIsm(ctx context.Context, ismId util.HexAddress) (types.ZKExecutionISM, error) {
	return k.isms.Get(ctx, ismId.GetInternalId())
}

// SetIsm is a test func used for setting an ISM in the isms store collection.
func (k *Keeper) SetIsm(ctx context.Context, ismId util.HexAddress, ism types.ZKExecutionISM) error {
	return k.isms.Set(ctx, ismId.GetInternalId(), ism)
}

// SetHeaderHash is a test func used for setting a header hash in the headers store collection.
func (k *Keeper) SetHeaderHash(ctx context.Context, height uint64, hash []byte) error {
	return k.headers.Set(ctx, height, hash)
}
