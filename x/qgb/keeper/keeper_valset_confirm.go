package keeper

import (
	"encoding/binary"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/cosmos/cosmos-sdk/store/prefix"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetValsetConfirm returns a valSet confirmation by a nonce and validator address
// Returns (nil, false, nil) if element not found.
func (k Keeper) GetValsetConfirm(
	ctx sdk.Context,
	nonce uint64,
	validator sdk.AccAddress,
) (*types.MsgValsetConfirm, bool, error) {
	store := ctx.KVStore(k.storeKey)
	if err := sdk.VerifyAddressFormat(validator); err != nil {
		return nil, false, err
	}
	entity := store.Get([]byte(types.GetValsetConfirmKey(nonce, validator)))
	if entity == nil {
		return nil, false, nil
	}
	confirm := types.MsgValsetConfirm{Nonce: nonce}
	k.cdc.MustUnmarshal(entity, &confirm)
	return &confirm, true, nil
}

// SetValsetConfirm sets a valset confirmation.
func (k Keeper) SetValsetConfirm(ctx sdk.Context, valsetConf types.MsgValsetConfirm) ([]byte, error) {
	store := ctx.KVStore(k.storeKey)
	addr, err := sdk.AccAddressFromBech32(valsetConf.Orchestrator)
	if err != nil {
		return nil, err
	}
	key := []byte(types.GetValsetConfirmKey(valsetConf.Nonce, addr))
	store.Set(key, k.cdc.MustMarshal(&valsetConf))
	return key, nil
}

// GetValsetConfirms get all ValsetConfirms with the provided nonce
// Returns empty array if no element is found.
func (k Keeper) GetValsetConfirms(ctx sdk.Context, nonce uint64) (confirms []types.MsgValsetConfirm) {
	prefixStore := prefix.NewStore(ctx.KVStore(k.storeKey), []byte(types.ValsetConfirmKey))
	start, end := PrefixRange([]byte(types.ConvertByteArrToString(types.UInt64Bytes(nonce))))
	iterator := prefixStore.Iterator(start, end)

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		confirm := types.MsgValsetConfirm{}
		k.cdc.MustUnmarshal(iterator.Value(), &confirm)
		confirms = append(confirms, confirm)
	}

	return confirms
}

// UInt64FromBytes create uint from binary big endian representation.
func UInt64FromBytes(s []byte) uint64 {
	return binary.BigEndian.Uint64(s)
}
