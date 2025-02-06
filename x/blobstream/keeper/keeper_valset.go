package keeper

import (
	"bytes"
	"fmt"
	"math/big"

	"cosmossdk.io/errors"
	cosmosmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v4/x/blobstream/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	gethcommon "github.com/ethereum/go-ethereum/common"
)

// GetLatestValset returns the latest validator set in store. This is different
// from the CurrentValset because this one has been saved and is therefore *the*
// latest valset saved in store. GetCurrentValset shows you what could be, if
// you chose to save it, this function shows you what is the latest valset that
// was saved. If not found, returns the current valset in case no valset exists
// in store after pruning. Otherwise panics.
func (k Keeper) GetLatestValset(ctx sdk.Context) (*types.Valset, error) {
	if !k.CheckLatestAttestationNonce(ctx) {
		return nil, types.ErrLatestAttestationNonceStillNotInitialized
	}
	if !k.CheckEarliestAvailableAttestationNonce(ctx) {
		return nil, types.ErrEarliestAvailableNonceStillNotInitialized
	}
	latestNonce := k.GetLatestAttestationNonce(ctx)
	earliestAvailableNonce := k.GetEarliestAvailableAttestationNonce(ctx)
	for i := latestNonce; i >= earliestAvailableNonce; i-- {
		at, found, err := k.GetAttestationByNonce(ctx, i)
		if err != nil {
			return nil, err
		}
		if !found {
			panic(errors.Wrap(
				types.ErrNilAttestation,
				fmt.Sprintf("stumbled upon nil attestation for nonce %d", i),
			))
		}
		valset, ok := at.(*types.Valset)
		if ok {
			return valset, nil
		}
	}
	if earliestAvailableNonce == 1 {
		// this means that the no pruning happened, but still the valset is
		// missing from the store
		panic(errors.Wrap(sdkerrors.ErrNotFound, "couldn't find latest valset"))
	}
	// this means that the latest valset was pruned and we can return the
	// current one as no significant changes to it happened
	currentVs, err := k.GetCurrentValset(ctx)
	return &currentVs, err
}

// SetLatestUnBondingBlockHeight sets the latest unbonding block height. Note
// this value is not saved to state or loaded at genesis. This value is reset to
// zero on chain upgrade.
func (k Keeper) SetLatestUnBondingBlockHeight(ctx sdk.Context, unbondingBlockHeight uint64) {
	store := ctx.KVStore(k.storeKey)
	store.Set([]byte(types.LatestUnBondingBlockHeight), types.UInt64Bytes(unbondingBlockHeight))
}

// GetLatestUnBondingBlockHeight returns the latest unbonding block height or
// zero if not set. This value is not saved or loaded at genesis. This value is
// reset to zero on chain upgrade.
func (k Keeper) GetLatestUnBondingBlockHeight(ctx sdk.Context) uint64 {
	store := ctx.KVStore(k.storeKey)
	bytes := store.Get([]byte(types.LatestUnBondingBlockHeight))

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
	// allocate enough space for all validators, but len zero, we then append so
	// that we have an array with extra capacity but the correct length
	// depending on how many validators have keys set.
	bridgeValidators := make([]*types.InternalBridgeValidator, 0, len(validators))
	totalPower := sdk.NewInt(0)
	for _, validator := range validators {
		val := validator.GetOperator()
		if err := sdk.VerifyAddressFormat(val); err != nil {
			return types.Valset{}, errors.Wrap(err, types.ErrInvalidValAddress.Error())
		}

		p := sdk.NewInt(k.StakingKeeper.GetLastValidatorPower(ctx, val))

		evmAddress, exists := k.GetEVMAddress(ctx, val)
		if !exists {
			// This should never happen and indicates a bug in the design of
			// the system (most likely that a hook wasn't called or a migration
			// for existing validators wasn't conducted). A validator
			// should always have an associated EVM address. Fortunately we can
			// safely recover from this by deriving the default again.
			ctx.Logger().Error("validator does not have an evm address set")
			evmAddress = types.DefaultEVMAddress(val)
			k.SetEVMAddress(ctx, val, evmAddress)
		}

		bv := types.BridgeValidator{Power: p.Uint64(), EvmAddress: evmAddress.Hex()}
		ibv, err := types.NewInternalBridgeValidator(bv)
		if err != nil {
			return types.Valset{}, errors.Wrapf(err, types.ErrInvalidEVMAddress.Error(), val)
		}
		bridgeValidators = append(bridgeValidators, ibv)
		totalPower = totalPower.Add(p)
	}
	// normalize power values to the maximum bridge power which is 2^32
	for i := range bridgeValidators {
		bridgeValidators[i].Power = normalizeValidatorPower(bridgeValidators[i].Power, totalPower)
	}

	// increment the nonce, since this potential future valset should be after
	// the current valset
	if !k.CheckLatestAttestationNonce(ctx) {
		return types.Valset{}, types.ErrLatestAttestationNonceStillNotInitialized
	}
	valsetNonce := k.GetLatestAttestationNonce(ctx) + 1

	valset, err := types.NewValset(valsetNonce, uint64(ctx.BlockHeight()), bridgeValidators, ctx.BlockTime())
	if err != nil {
		return types.Valset{}, (errors.Wrap(err, types.ErrInvalidValset.Error()))
	}
	return *valset, nil
}

// normalizeValidatorPower scales rawPower with respect to totalValidatorPower
// to take a value between 0 and 2^32.
// Uses BigInt operations to avoid overflow errors.
// Example: rawPower = max (2^63 - 1), totalValidatorPower = 1, validator: (2^63 - 1)
//
//	result: (2^63 - 1) * 2^32 / (2^63 - 1) = 2^32 = 4294967296 [this is the multiplier value below, our max output]
//
// Example: rawPower = max (2^63 - 1), totalValidatorPower = 1000 validators with the same power: 1000*(2^63 - 1)
//
//	result: (2^63 - 1) * 2^32 / (1000(2^63 - 1)) = 2^32 / 1000 = 4294967
//
// This is using the min-max normalization https://en.wikipedia.org/wiki/Feature_scaling
// from the interval [0, total validator power] to [0, 2^32].
// Check the `PowerDiff` method under `types.validator.go` for more information.
func normalizeValidatorPower(rawPower uint64, totalValidatorPower cosmosmath.Int) uint64 {
	// Compute rawPower * multiplier / quotient Set the upper limit to 2^32,
	// which would happen if there is a single validator with all the power
	multiplier := new(big.Int).SetUint64(4294967296)
	// Scale by current validator powers, a particularly low-power validator (1
	// out of over 2^32) would have 0 power
	quotient := new(big.Int).Set(totalValidatorPower.BigInt())
	power := new(big.Int).SetUint64(rawPower)
	power.Mul(power, multiplier)
	power.Quo(power, quotient)
	return power.Uint64()
}

// GetLatestValsetBeforeNonce returns the previous valset before the provided
// `nonce`. the `nonce` can be a valset, but this method will return the valset
// before it. If the provided nonce is 1, it will return an error, because,
// there is no valset before nonce 1.
func (k Keeper) GetLatestValsetBeforeNonce(ctx sdk.Context, nonce uint64) (*types.Valset, error) {
	if !k.CheckLatestAttestationNonce(ctx) {
		return nil, types.ErrLatestAttestationNonceStillNotInitialized
	}
	if !k.CheckEarliestAvailableAttestationNonce(ctx) {
		return nil, types.ErrEarliestAvailableNonceStillNotInitialized
	}
	if nonce == 1 {
		return nil, types.ErrNoValsetBeforeNonceOne
	}
	earliestAvailableNonce := k.GetEarliestAvailableAttestationNonce(ctx)
	if nonce < earliestAvailableNonce {
		return nil, types.ErrRequestedNonceWasPruned
	}
	if nonce > k.GetLatestAttestationNonce(ctx) {
		return nil, types.ErrNonceHigherThanLatestAttestationNonce
	}
	// starting at nonce-1 because the current nonce can be a valset and we need
	// the previous one.
	for i := nonce - 1; i >= earliestAvailableNonce; i-- {
		at, found, err := k.GetAttestationByNonce(ctx, i)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, errors.Wrap(
				types.ErrNilAttestation,
				fmt.Sprintf("nonce=%d", i),
			)
		}
		valset, ok := at.(*types.Valset)
		if ok {
			return valset, nil
		}
	}
	return nil, errors.Wrap(
		sdkerrors.ErrNotFound,
		fmt.Sprintf("couldn't find valset before nonce %d", nonce),
	)
}

func (k Keeper) SetEVMAddress(ctx sdk.Context, valAddress sdk.ValAddress, evmAddress gethcommon.Address) {
	store := ctx.KVStore(k.storeKey)
	store.Set(types.GetEVMKey(valAddress), evmAddress.Bytes())
}

func (k Keeper) GetEVMAddress(ctx sdk.Context, valAddress sdk.ValAddress) (gethcommon.Address, bool) {
	store := ctx.KVStore(k.storeKey)
	if !store.Has(types.GetEVMKey(valAddress)) {
		return gethcommon.Address{}, false
	}
	addrBytes := store.Get(types.GetEVMKey(valAddress))
	return gethcommon.BytesToAddress(addrBytes), true
}

// IsEVMAddressUnique checks if the provided evm address is globally unique. This
// includes the defaults we set validators when they initially create a validator
// before registering
func (k Keeper) IsEVMAddressUnique(ctx sdk.Context, evmAddress gethcommon.Address) bool {
	store := ctx.KVStore(k.storeKey)
	addrBytes := evmAddress.Bytes()
	iterator := sdk.KVStorePrefixIterator(store, []byte(types.EVMAddress))
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		if bytes.Equal(iterator.Value(), addrBytes) {
			return false
		}
	}
	return true
}
