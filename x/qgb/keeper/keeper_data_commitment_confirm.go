package keeper

import (
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"strconv"
)

// TODO add unit tests for all the keepers

// GetDataCommitmentConfirm Returns a data commitment confirm by nonce and validator address
// nonce = endBlock % data window in decimal base
func (k Keeper) GetDataCommitmentConfirm(
	ctx sdk.Context,
	endBlock uint64,
	beginBlock uint64,
	validator sdk.AccAddress,
) *types.MsgDataCommitmentConfirm {
	store := ctx.KVStore(k.storeKey)
	if err := sdk.VerifyAddressFormat(validator); err != nil {
		ctx.Logger().Error("invalid validator address")
		return nil
	}
	key := store.Get([]byte(types.GetDataCommitmentConfirmKey(endBlock, beginBlock, validator)))
	if key == nil {
		return nil
	}
	confirm := types.MsgDataCommitmentConfirm{}
	k.cdc.MustUnmarshal(key, &confirm)
	return &confirm
}

// GetDataCommitmentConfirmsByCommitment Returns data commitment confirms by nonce
// Too heavy, shouldn't be primarily used
func (k Keeper) GetDataCommitmentConfirmsByCommitment(
	ctx sdk.Context,
	commitment string,
) (confirms []types.MsgDataCommitmentConfirm) {
	store := ctx.KVStore(k.storeKey)
	iterator := store.Iterator(nil, nil)

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		confirm := types.MsgDataCommitmentConfirm{}
		err := k.cdc.Unmarshal(iterator.Value(), &confirm)
		if err != nil {
			continue
		}
		if commitment == confirm.Commitment {
			confirms = append(confirms, confirm)
		}
	}

	return confirms
}

// GetDataCommitmentConfirmsByExactRange Returns data commitment confirms by the provided exact range
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

// SetDataCommitmentConfirm Sets the data commitment confirm and indexes it by commitment and validator address
func (k Keeper) SetDataCommitmentConfirm(ctx sdk.Context, dcConf types.MsgDataCommitmentConfirm) []byte {
	store := ctx.KVStore(k.storeKey)
	addr, err := sdk.AccAddressFromBech32(dcConf.ValidatorAddress)
	if err != nil {
		panic(err)
	}
	key := []byte(types.GetDataCommitmentConfirmKey(dcConf.EndBlock, dcConf.BeginBlock, addr))
	store.Set(key, k.cdc.MustMarshal(&dcConf))
	return key
}

// DeleteDataCommitmentConfirms deletes a data commitment confirm by range and validator address
func (k Keeper) DeleteDataCommitmentConfirms(
	ctx sdk.Context,
	endBlock uint64,
	beginBlock uint64,
	validator sdk.AccAddress,
) {
	store := ctx.KVStore(k.storeKey)
	if err := sdk.VerifyAddressFormat(validator); err != nil {
		ctx.Logger().Error("invalid validator address")
		return
	}
	key := store.Get([]byte(types.GetDataCommitmentConfirmKey(endBlock, beginBlock, validator)))
	if key == nil {
		return
	}
	if store.Has(key) {
		store.Delete(key)
	}
}
