package keeper

import (
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetDataCommitmentConfirm Returns a data commitment confirm by commitment and validator address
func (k Keeper) GetDataCommitmentConfirm(
	ctx sdk.Context,
	commitment string,
	validator sdk.AccAddress,
) *types.MsgDataCommitmentConfirm {
	store := ctx.KVStore(k.storeKey)
	if err := sdk.VerifyAddressFormat(validator); err != nil {
		ctx.Logger().Error("invalid validator address")
		return nil
	}
	key := store.Get([]byte(types.GetDataCommitmentConfirmKey(commitment, validator)))
	if key == nil {
		return nil
	}
	confirm := types.MsgDataCommitmentConfirm{}
	k.cdc.MustUnmarshal(key, &confirm)
	return &confirm
}

// // GetDataCommitmentConfirmsByCommitment Returns data commitment confirms by commitment
// func (k Keeper) GetDataCommitmentConfirmsByCommitment(
// 	ctx sdk.Context,
// 	commitment string,
// ) (confirms []types.MsgDataCommitmentConfirm) {
// 	prefixStore := prefix.NewStore(ctx.KVStore(k.storeKey), []byte(types.DataCommitmentConfirmKey))
// 	start, end := prefixRange([]byte(commitment)) // How does this work?
// 	iterator := prefixStore.Iterator(start, end)

// 	defer iterator.Close()

// 	for ; iterator.Valid(); iterator.Next() {
// 		confirm := types.MsgDataCommitmentConfirm{}
// 		k.cdc.MustUnmarshal(iterator.Value(), &confirm)
// 		confirms = append(confirms, confirm)
// 	}

// 	return confirms
// }

// GetDataCommitmentConfirmsByValidator Returns data commitment confirms by validator address
func (k Keeper) GetDataCommitmentConfirmsByValidator(
	ctx sdk.Context,
	validator sdk.AccAddress,
) (confirms []types.MsgDataCommitmentConfirm) {
	if err := sdk.VerifyAddressFormat(validator); err != nil {
		ctx.Logger().Error("invalid validator address")
		return nil
	}

	prefixStore := prefix.NewStore(ctx.KVStore(k.storeKey), []byte(types.DataCommitmentConfirmKey))
	start, end := prefixRange([]byte(types.DataCommitmentConfirmKey)) // FIXME can we make this faster ?
	iterator := prefixStore.Iterator(start, end)

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		confirm := types.MsgDataCommitmentConfirm{}
		k.cdc.MustUnmarshal(iterator.Value(), &confirm)
		if confirm.ValidatorAddress == validator.String() {
			confirms = append(confirms, confirm)
		}
	}

	return confirms
}

// GetDataCommitmentConfirmsByRange Returns data commitment confirms by the provided range
func (k Keeper) GetDataCommitmentConfirmsByRange(
	ctx sdk.Context,
	beginBlock int64,
	endBlock int64,
) (confirms []types.MsgDataCommitmentConfirm) {
	prefixStore := prefix.NewStore(ctx.KVStore(k.storeKey), []byte(types.DataCommitmentConfirmKey))
	start, end := prefixRange([]byte(types.DataCommitmentConfirmKey)) // FIXME can we make this faster ?
	iterator := prefixStore.Iterator(start, end)

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		confirm := types.MsgDataCommitmentConfirm{}
		k.cdc.MustUnmarshal(iterator.Value(), &confirm)
		if confirm.BeginBlock <= beginBlock && confirm.EndBlock >= endBlock {
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
	key := []byte(types.GetDataCommitmentConfirmKey(dcConf.Commitment, addr))
	store.Set(key, k.cdc.MustMarshal(&dcConf))
	return key
}

// DeleteDataCommitmentConfirms deletes a data commitment confirm by commitment and validator address
func (k Keeper) DeleteDataCommitmentConfirms(ctx sdk.Context, commitment string, validator sdk.AccAddress) {
	store := ctx.KVStore(k.storeKey)
	if err := sdk.VerifyAddressFormat(validator); err != nil {
		ctx.Logger().Error("invalid validator address")
		return
	}
	key := store.Get([]byte(types.GetDataCommitmentConfirmKey(commitment, validator)))
	if key == nil {
		return
	}
	if store.Has(key) {
		store.Delete(key)
	}
}
