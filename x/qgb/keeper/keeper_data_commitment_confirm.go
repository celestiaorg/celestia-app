package keeper

import (
	"strconv"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// TODO add unit tests for all the keepers

// GetDataCommitmentConfirm Returns a data commitment confirm by nonce and validator address
// nonce = endBlock % data window in decimal base
// Returns (nil, false, nil) if element not found.
func (k Keeper) GetDataCommitmentConfirm(
	ctx sdk.Context,
	endBlock uint64,
	beginBlock uint64,
	validator sdk.AccAddress,
) (*types.MsgDataCommitmentConfirm, bool, error) {
	store := ctx.KVStore(k.storeKey)
	if err := sdk.VerifyAddressFormat(validator); err != nil {
		return nil, false, err
	}
	bConf := store.Get([]byte(types.GetDataCommitmentConfirmKey(endBlock, beginBlock, validator)))
	if bConf == nil {
		return nil, false, nil
	}
	confirm := types.MsgDataCommitmentConfirm{}
	k.cdc.MustUnmarshal(bConf, &confirm)
	return &confirm, true, nil
}

// GetDataCommitmentConfirmsByExactRange Returns data commitment confirms by the provided exact range.
// Returns empty array if no element is found.
func (k Keeper) GetDataCommitmentConfirmsByExactRange(
	ctx sdk.Context,
	beginBlock uint64,
	endBlock uint64,
) (confirms []types.MsgDataCommitmentConfirm) {
	store := ctx.KVStore(k.storeKey)
	iterator := sdk.KVStorePrefixIterator(
		store,
		[]byte(types.DataCommitmentConfirmKey+
			strconv.FormatInt(int64(endBlock), 16)+
			strconv.FormatInt(int64(beginBlock), 16),
		),
	)

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		confirm := types.MsgDataCommitmentConfirm{}
		err := k.cdc.Unmarshal(iterator.Value(), &confirm)
		if err != nil {
			continue
		}
		if beginBlock == confirm.BeginBlock && endBlock == confirm.EndBlock {
			confirms = append(confirms, confirm)
		}
	}

	return confirms
}

// SetDataCommitmentConfirm Sets the data commitment confirm and indexes it by commitment and validator address.
func (k Keeper) SetDataCommitmentConfirm(ctx sdk.Context, dcConf types.MsgDataCommitmentConfirm) ([]byte, error) {
	store := ctx.KVStore(k.storeKey)
	addr, err := sdk.AccAddressFromBech32(dcConf.ValidatorAddress)
	if err != nil {
		return nil, err
	}
	key := []byte(types.GetDataCommitmentConfirmKey(dcConf.EndBlock, dcConf.BeginBlock, addr))
	store.Set(key, k.cdc.MustMarshal(&dcConf))
	return key, nil
}
