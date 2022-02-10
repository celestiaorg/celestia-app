package keeper

import (
	"fmt"

	"github.com/tendermint/tendermint/libs/log"

	"github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Keeper handles all the state changes for the celestia-app module.
type Keeper struct {
	cdc      codec.BinaryCodec
	storeKey sdk.StoreKey
	memKey   sdk.StoreKey
	bank     BankKeeper
}

func NewKeeper(cdc codec.BinaryCodec, storeKey, memKey sdk.StoreKey) *Keeper {
	return &Keeper{
		cdc:      cdc,
		storeKey: storeKey,
		memKey:   memKey,
	}
}

func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

/////////////////////////////
//     VALSET CONFIRMS     //
/////////////////////////////

// GetValsetConfirm returns a valset confirmation by a nonce and validator address
func (k Keeper) GetValsetConfirm(ctx sdk.Context, nonce uint64, validator sdk.AccAddress) *types.MsgValsetConfirm {
	store := ctx.KVStore(k.storeKey)
	if err := sdk.VerifyAddressFormat(validator); err != nil {
		ctx.Logger().Error("invalid validator address")
		return nil
	}
	entity := store.Get([]byte(types.GetValsetConfirmKey(nonce, validator)))
	if entity == nil {
		return nil
	}
	confirm := types.MsgValsetConfirm{
		Nonce:        nonce,
		Orchestrator: "",
		EthAddress:   "",
		Signature:    "",
	}
	k.cdc.MustUnmarshal(entity, &confirm)
	return &confirm
}

// SetValsetConfirm sets a valset confirmation
func (k Keeper) SetValsetConfirm(ctx sdk.Context, valsetConf types.MsgValsetConfirm) []byte {
	store := ctx.KVStore(k.storeKey)
	addr, err := sdk.AccAddressFromBech32(valsetConf.Orchestrator)
	if err != nil {
		panic(err)
	}
	key := []byte(types.GetValsetConfirmKey(valsetConf.Nonce, addr))
	store.Set(key, k.cdc.MustMarshal(&valsetConf))
	return key
}

// GetValsetConfirms returns all validator set confirmations by nonce
func (k Keeper) GetValsetConfirms(ctx sdk.Context, nonce uint64) (confirms []types.MsgValsetConfirm) {
	prefixStore := prefix.NewStore(ctx.KVStore(k.storeKey), []byte(types.ValsetConfirmKey))
	start, end := prefixRange([]byte(types.ConvertByteArrToString(types.UInt64Bytes(nonce))))
	iterator := prefixStore.Iterator(start, end)

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		confirm := types.MsgValsetConfirm{
			Nonce:        nonce,
			Orchestrator: "",
			EthAddress:   "",
			Signature:    "",
		}
		k.cdc.MustUnmarshal(iterator.Value(), &confirm)
		confirms = append(confirms, confirm)
	}

	return confirms
}

// DeleteValsetConfirms deletes the valset confirmations for the valset at a given nonce from state
func (k Keeper) DeleteValsetConfirms(ctx sdk.Context, nonce uint64) {
	store := ctx.KVStore(k.storeKey)
	for _, confirm := range k.GetValsetConfirms(ctx, nonce) {
		orchestrator, err := sdk.AccAddressFromBech32(confirm.Orchestrator)
		if err == nil {
			confirmKey := []byte(types.GetValsetConfirmKey(nonce, orchestrator))
			if store.Has(confirmKey) {
				store.Delete(confirmKey)
			}
		}
	}
}
