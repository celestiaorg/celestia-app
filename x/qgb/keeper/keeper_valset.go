package keeper

import (
	"fmt"
	"math/big"
	"sort"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SetValsetRequest returns a new instance of the Gravity BridgeValidatorSet
// by taking a snapshot of the current set, this validator set is also placed
// into the store to be signed by validators and submitted to Ethereum. This
// is the only function to call when you want to create a validator set that
// is signed by consensus. If you want to peek at the present state of the set
// and perhaps take action based on that use k.GetCurrentValset
// i.e. {"nonce": 1, "members": [{"eth_addr": "foo", "power": 11223}]}
func (k Keeper) SetValsetRequest(ctx sdk.Context) types.Valset {
	valset, err := k.GetCurrentValset(ctx)
	if err != nil {
		panic(err)
	}
	k.StoreValset(ctx, valset)
	k.SetLatestValsetNonce(ctx, valset.Nonce)

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeValsetRequest,
			sdk.NewAttribute(sdk.AttributeKeyModule, types.ModuleName),
			sdk.NewAttribute(types.AttributeKeyNonce, fmt.Sprint(valset.Nonce)),
		),
	)

	return valset
}

// StoreValset is for storing a valiator set at a given height, once this function is called
// the validator set will be available to the Ethereum Signers (orchestrators) to submit signatures
// therefore this function will panic if you attempt to overwrite an existing key. Any changes to
// historical valsets can not possibly be correct, as it would invalidate the signatures. The only
// valid operation on the same index is store followed by delete when it is time to prune state
func (k Keeper) StoreValset(ctx sdk.Context, valset types.Valset) {
	key := []byte(types.GetValsetKey(valset.Nonce))
	store := ctx.KVStore(k.storeKey)

	if store.Has(key) {
		panic("Trying to overwrite existing valset!")
	}

	store.Set((key), k.cdc.MustMarshal(&valset))
}

// HasValsetRequest returns true if a valset defined by a nonce exists
func (k Keeper) HasValsetRequest(ctx sdk.Context, nonce uint64) bool {
	store := ctx.KVStore(k.storeKey)
	return store.Has([]byte(types.GetValsetKey(nonce)))
}

// DeleteValset deletes the valset at a given nonce from state
func (k Keeper) DeleteValset(ctx sdk.Context, nonce uint64) {
	ctx.KVStore(k.storeKey).Delete([]byte(types.GetValsetKey(nonce)))
}

// CheckLatestValsetNonce returns true if the latest valset nonce
// is declared in the store and false if it has not been initialized
func (k Keeper) CheckLatestValsetNonce(ctx sdk.Context) bool {
	store := ctx.KVStore(k.storeKey)
	has := store.Has([]byte(types.LatestValsetNonce))
	return has
}

// GetLatestValsetNonce returns the latest valset nonce
func (k Keeper) GetLatestValsetNonce(ctx sdk.Context) uint64 {
	if !k.CheckLatestValsetNonce(ctx) {
		// TODO: handle this case for genesis properly. Note for Evan: write an issue
		return 0
	}

	store := ctx.KVStore(k.storeKey)
	bytes := store.Get([]byte(types.LatestValsetNonce))
	return UInt64FromBytes(bytes)
}

// SetLatestValsetNonce sets the latest valset nonce, since it's
// expected that this value will only increase it panics on an attempt
// to decrement
func (k Keeper) SetLatestValsetNonce(ctx sdk.Context, nonce uint64) {
	// this is purely an increasing counter and should never decrease
	if k.CheckLatestValsetNonce(ctx) && k.GetLatestValsetNonce(ctx) > nonce {
		panic("Decrementing valset nonce!")
	}

	store := ctx.KVStore(k.storeKey)
	store.Set([]byte(types.LatestValsetNonce), types.UInt64Bytes(nonce))
}

// GetValset returns a valset by nonce
func (k Keeper) GetValset(ctx sdk.Context, nonce uint64) *types.Valset {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get([]byte(types.GetValsetKey(nonce)))
	if bz == nil {
		return nil
	}
	var valset types.Valset
	k.cdc.MustUnmarshal(bz, &valset)
	return &valset
}

// IterateValsets retruns all valsetRequests
func (k Keeper) IterateValsets(ctx sdk.Context, cb func(key []byte, val *types.Valset) bool) {
	prefixStore := prefix.NewStore(ctx.KVStore(k.storeKey), []byte(types.ValsetRequestKey))
	iter := prefixStore.ReverseIterator(nil, nil)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		var valset types.Valset
		k.cdc.MustUnmarshal(iter.Value(), &valset)
		// cb returns true to stop early
		if cb(iter.Key(), &valset) {
			break
		}
	}
}

// GetValsets returns all the validator sets in state
func (k Keeper) GetValsets(ctx sdk.Context) (out []types.Valset) {
	// TODO this should definitely be optimized. Adding support for paging or providing a range
	// is way better
	k.IterateValsets(ctx, func(_ []byte, val *types.Valset) bool {
		out = append(out, *val)
		return false
	})
	sort.Sort(types.Valsets(out))
	return
}

// GetLatestValset returns the latest validator set in store. This is different
// from the CurrentValset because this one has been saved and is therefore *the* valset
// for this nonce. GetCurrentValset shows you what could be, if you chose to save it, this function
// shows you what is the latest valset that was saved.
func (k Keeper) GetLatestValset(ctx sdk.Context) (out *types.Valset) {
	latestValsetNonce := k.GetLatestValsetNonce(ctx)
	if latestValsetNonce == 0 {
		valset := k.SetValsetRequest(ctx)
		return &valset
	}

	out = k.GetValset(ctx, latestValsetNonce)
	return
}

// SetLastUnBondingBlockHeight sets the last unbonding block height. Note this value is not saved and loaded in genesis
// and is reset to zero on chain upgrade.
func (k Keeper) SetLastUnBondingBlockHeight(ctx sdk.Context, unbondingBlockHeight uint64) {
	store := ctx.KVStore(k.storeKey)
	store.Set([]byte(types.LastUnBondingBlockHeight), types.UInt64Bytes(unbondingBlockHeight))
}

// GetLastUnBondingBlockHeight returns the last unbonding block height, returns zero if not set, this is not
// saved or loaded in genesis and is reset to zero on chain upgrade
func (k Keeper) GetLastUnBondingBlockHeight(ctx sdk.Context) uint64 {
	store := ctx.KVStore(k.storeKey)
	bytes := store.Get([]byte(types.LastUnBondingBlockHeight))

	if len(bytes) == 0 {
		return 0
	}
	return UInt64FromBytes(bytes)
}

func (k Keeper) GetCurrentValset(ctx sdk.Context) (types.Valset, error) {
	validators := k.StakingKeeper.GetBondedValidatorsByPower(ctx)
	if len(validators) == 0 {
		return types.Valset{}, types.ErrNoValidators
	}
	// allocate enough space for all validators, but len zero, we then append
	// so that we have an array with extra capacity but the correct length depending
	// on how many validators have keys set.
	bridgeValidators := make([]*types.InternalBridgeValidator, 0, len(validators))
	totalPower := sdk.NewInt(0)
	// TODO someone with in depth info on Cosmos staking should determine
	// if this is doing what I think it's doing
	for _, validator := range validators {
		val := validator.GetOperator()
		if err := sdk.VerifyAddressFormat(val); err != nil {
			return types.Valset{}, sdkerrors.Wrap(err, types.ErrInvalidValAddress.Error())
		}

		p := sdk.NewInt(k.StakingKeeper.GetLastValidatorPower(ctx, val))

		// TODO make sure this  is always the case
		bv := types.BridgeValidator{Power: p.Uint64(), EthereumAddress: validator.EthAddress}
		ibv, err := types.NewInternalBridgeValidator(bv)
		if err != nil {
			return types.Valset{}, sdkerrors.Wrapf(err, types.ErrInvalidEthAddress.Error(), val)
		}
		bridgeValidators = append(bridgeValidators, ibv)
		totalPower = totalPower.Add(p)
	}
	// normalize power values to the maximum bridge power which is 2^32
	for i := range bridgeValidators {
		bridgeValidators[i].Power = normalizeValidatorPower(bridgeValidators[i].Power, totalPower)
	}

	// increment the nonce, since this potential future valset should be after the current valset
	valsetNonce := k.GetLatestValsetNonce(ctx) + 1

	valset, err := types.NewValset(valsetNonce, uint64(ctx.BlockHeight()), bridgeValidators)
	if err != nil {
		return types.Valset{}, (sdkerrors.Wrap(err, types.ErrInvalidValset.Error()))
	}
	return *valset, nil
}

// normalizeValidatorPower scales rawPower with respect to totalValidatorPower to take a value between 0 and 2^32
// Uses BigInt operations to avoid overflow errors
// Example: rawPower = max (2^63 - 1), totalValidatorPower = 1 validator: (2^63 - 1)
//   result: (2^63 - 1) * 2^32 / (2^63 - 1) = 2^32 = 4294967296 [this is the multiplier value below, our max output]
// Example: rawPower = max (2^63 - 1), totalValidatorPower = 1000 validators with the same power: 1000*(2^63 - 1)
//   result: (2^63 - 1) * 2^32 / (1000(2^63 - 1)) = 2^32 / 1000 = 4294967
func normalizeValidatorPower(rawPower uint64, totalValidatorPower sdk.Int) uint64 {
	// Compute rawPower * multiplier / quotient
	// Set the upper limit to 2^32, which would happen if there is a single validator with all the power
	multiplier := new(big.Int).SetUint64(4294967296)
	// Scale by current validator powers, a particularly low-power validator (1 out of over 2^32) would have 0 power
	quotient := new(big.Int).Set(totalValidatorPower.BigInt())
	power := new(big.Int).SetUint64(rawPower)
	power.Mul(power, multiplier)
	power.Quo(power, quotient)
	return power.Uint64()
}
