package keeper

import (
	"context"

	"github.com/bcp-innovations/hyperlane-cosmos/util"

	"github.com/celestiaorg/celestia-app/v4/x/zkism/types"
)

// SetIsm is a test func used for setting an ISM in the store collection.
func (k *Keeper) SetIsm(ctx context.Context, ismId util.HexAddress, ism types.ZKExecutionISM) error {
	return k.isms.Set(ctx, ismId.GetInternalId(), ism)
}
