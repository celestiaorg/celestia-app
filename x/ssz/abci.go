package ssz

import (
	"github.com/celestiaorg/celestia-app/x/ssz/keeper"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func EndBlocker(ctx sdk.Context, k keeper.Keeper) {
	h, err := k.CurrentValsetSSZHash(ctx)
	if err != nil {
		// TODO: maybe just log error
		panic(err)
	}
	k.SetSSZHash(ctx, h)
}
