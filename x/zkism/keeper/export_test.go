package keeper

import (
	"context"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
)

// SetIsm is a test func used for setting an ISM in the store collection.
func (k *Keeper) SetIsm(ctx context.Context, ismId util.HexAddress, ism types.ZKExecutionISM) error {
	return k.isms.Set(ctx, ismId.GetInternalId(), ism)
}

// SetHeaderHash is a test func used for setting a header hash in the store collection.
func (k *Keeper) SetHeaderHash(ctx context.Context, height uint64, hash []byte) error {
	return k.headers.Set(ctx, height, hash)
}
