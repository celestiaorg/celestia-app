package keeper

import (
	"github.com/celestiaorg/celestia-app/x/qgb/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetValsetConfirm
func (k Keeper) GetValsetConfirm(ctx sdk.Context, nonce uint64, validator sdk.AccAddress) *types.MsgValsetConfirm {
	// TODO
	return nil
}

// SetValsetConfirm
func (k Keeper) SetValsetConfirm(ctx sdk.Context, valsetConf types.MsgValsetConfirm) []byte {
	// TODO
	return nil
}

// GetValsetConfirms
func (k Keeper) GetValsetConfirms(ctx sdk.Context, nonce uint64) (confirms []types.MsgValsetConfirm) {
	// TODO
	return nil
}

// DeleteValsetConfirms
func (k Keeper) DeleteValsetConfirms(ctx sdk.Context, nonce uint64) {
	// TODO
}
