package keeper

import (
	"fmt"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"sort"
)

// TODO add unit tests for alll the keepers

// SetDataCommitmentRequest Sets a new data commitment request to the store to be signed
// by orchestrators afterwards.
func (k Keeper) SetDataCommitmentRequest(ctx sdk.Context) types.DataCommitment {
	dataCommitment, err := k.GetCurrentDataCommitment(ctx)
	if err != nil {
		panic(err)
	}
	k.StoreDataCommitment(ctx, dataCommitment)
	k.SetLatestDataCommitmentNonce(ctx, dataCommitment.Nonce)

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeDataCommitmentRequest,
			sdk.NewAttribute(sdk.AttributeKeyModule, types.ModuleName),
			sdk.NewAttribute(types.AttributeKeyNonce, fmt.Sprint(dataCommitment.Nonce)),
		),
	)

	return dataCommitment
}

// GetCurrentDataCommitment Creates latest data commitment at current height according to
// the data commitment window specified
func (k Keeper) GetCurrentDataCommitment(ctx sdk.Context) (types.DataCommitment, error) {
	beginBlock := uint64(ctx.BlockHeight()) - types.DataCommitmentWindow
	endBlock := uint64(ctx.BlockHeight())
	nonce := uint64(ctx.BlockHeight()) / types.DataCommitmentWindow

	dataCommitment := types.NewDataCommitment(nonce, beginBlock, endBlock)
	return *dataCommitment, nil
}

// StoreDataCommitment
func (k Keeper) StoreDataCommitment(ctx sdk.Context, dc types.DataCommitment) {
	key := []byte(types.GetDataCommitmentKey(dc.Nonce))
	store := ctx.KVStore(k.storeKey)

	if store.Has(key) {
		panic("Trying to overwrite existing data commitment request!")
	}

	store.Set((key), k.cdc.MustMarshal(&dc))
}

// SetLatestDataCommitmentNonce sets the latest data commitment nonce, since it's
// expected that this value will only increase it panics on an attempt
// to decrement
func (k Keeper) SetLatestDataCommitmentNonce(ctx sdk.Context, nonce uint64) {
	// this is purely an increasing counter and should never decrease
	if k.CheckLatestValsetNonce(ctx) && k.GetLatestValsetNonce(ctx) > nonce {
		panic("Decrementing data commitment nonce!")
	}

	store := ctx.KVStore(k.storeKey)
	store.Set([]byte(types.LatestDataCommitmentNonce), types.UInt64Bytes(nonce))
}

// CheckLatestDataCommitmentNonce returns true if the latest data commitment nonce
// is declared in the store and false if it has not been initialized
func (k Keeper) CheckLatestDataCommitmentNonce(ctx sdk.Context) bool {
	store := ctx.KVStore(k.storeKey)
	has := store.Has([]byte(types.LatestDataCommitmentNonce))
	return has
}

// GetLatestDataCommitmentNonce returns the latest data commitment nonce
func (k Keeper) GetLatestDataCommitmentNonce(ctx sdk.Context) uint64 {
	if !k.CheckLatestDataCommitmentNonce(ctx) {
		// TODO: handle this case for genesis properly. Note for Evan: write an issue
		return 0
	}

	store := ctx.KVStore(k.storeKey)
	bytes := store.Get([]byte(types.LatestDataCommitmentNonce))
	return UInt64FromBytes(bytes)
}

// GetDataCommitment returns a data commitment by nonce
func (k Keeper) GetDataCommitment(ctx sdk.Context, nonce uint64) *types.DataCommitment {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get([]byte(types.GetDataCommitmentKey(nonce)))
	if bz == nil {
		return nil
	}
	var dc types.DataCommitment
	k.cdc.MustUnmarshal(bz, &dc)
	return &dc
}

// DataCommitments is a collection of DataCommitment
type DataCommitments []types.DataCommitment

// GetDataCommitments returns all the data commitments in state
func (k Keeper) GetDataCommitments(ctx sdk.Context) (out []types.DataCommitment) {
	// TODO this should definitely be optimized. Adding support for paging or providing a range
	// is way better
	k.IterateDataCommitments(ctx, func(_ []byte, val *types.DataCommitment) bool {
		out = append(out, *val)
		return false
	})
	sort.Sort(types.DataCommitments(out))
	return
}

// IterateDataCommitments retruns all DataCommitmentRequests
func (k Keeper) IterateDataCommitments(ctx sdk.Context, cb func(key []byte, val *types.DataCommitment) bool) {
	prefixStore := prefix.NewStore(ctx.KVStore(k.storeKey), []byte(types.DataCommitmentRequestKey))
	iter := prefixStore.ReverseIterator(nil, nil)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		var dc types.DataCommitment
		k.cdc.MustUnmarshal(iter.Value(), &dc)
		// cb returns true to stop early
		if cb(iter.Key(), &dc) {
			break
		}
	}
}
