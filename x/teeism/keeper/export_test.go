package keeper

import (
	"context"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
)

// RecordRootForTest attests root on an ism the way SubmitAttestation records
// it, for tests that need roots no real quote commits to.
func (k *Keeper) RecordRootForTest(ctx context.Context, id util.HexAddress, root []byte) error {
	ism, err := k.isms.Get(ctx, id.GetInternalId())
	if err != nil {
		return err
	}
	if err := k.recordRoot(ctx, &ism, root); err != nil {
		return err
	}
	ism.TreeRoot = root
	ism.TreeCount = 7
	return k.isms.Set(ctx, id.GetInternalId(), ism)
}
