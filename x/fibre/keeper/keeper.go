package keeper

import (
	"context"

	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v6/x/fibre/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// Keeper handles all the state changes for the fibre module.
type Keeper struct {
	cdc          codec.Codec
	storeKey     storetypes.StoreKey
	stakingKeeper StakingKeeper
}

// StakingKeeper defines the expected interface needed to verify validator status
type StakingKeeper interface {
	GetValidator(ctx context.Context, addr sdk.ValAddress) (validator stakingtypes.Validator, err error)
	GetBondedValidatorsByPower(ctx context.Context) ([]stakingtypes.Validator, error)
}

func NewKeeper(
	cdc codec.Codec,
	storeKey storetypes.StoreKey,
	stakingKeeper StakingKeeper,
) *Keeper {
	return &Keeper{
		cdc:           cdc,
		storeKey:      storeKey,
		stakingKeeper: stakingKeeper,
	}
}

// SetFibreProviderInfo sets fibre provider info for a validator
func (k Keeper) SetFibreProviderInfo(ctx context.Context, validatorAddr sdk.ValAddress, info types.FibreProviderInfo) {
	store := k.getStore(ctx)
	key := types.FibreProviderInfoStoreKey(validatorAddr.String())
	bz := k.cdc.MustMarshal(&info)
	store.Set(key, bz)
}

// GetFibreProviderInfo gets fibre provider info for a validator
func (k Keeper) GetFibreProviderInfo(ctx context.Context, validatorAddr sdk.ValAddress) (types.FibreProviderInfo, bool) {
	store := k.getStore(ctx)
	key := types.FibreProviderInfoStoreKey(validatorAddr.String())
	
	bz := store.Get(key)
	if bz == nil {
		return types.FibreProviderInfo{}, false
	}

	var info types.FibreProviderInfo
	k.cdc.MustUnmarshal(bz, &info)
	return info, true
}

// RemoveFibreProviderInfo removes fibre provider info for a validator
func (k Keeper) RemoveFibreProviderInfo(ctx context.Context, validatorAddr sdk.ValAddress) {
	store := k.getStore(ctx)
	key := types.FibreProviderInfoStoreKey(validatorAddr.String())
	store.Delete(key)
}

// HasFibreProviderInfo checks if a validator has fibre provider info
func (k Keeper) HasFibreProviderInfo(ctx context.Context, validatorAddr sdk.ValAddress) bool {
	store := k.getStore(ctx)
	key := types.FibreProviderInfoStoreKey(validatorAddr.String())
	return store.Has(key)
}

// IsValidatorActive checks if a validator is in the active (bonded) set
func (k Keeper) IsValidatorActive(ctx context.Context, validatorAddr sdk.ValAddress) (bool, error) {
	validator, err := k.stakingKeeper.GetValidator(ctx, validatorAddr)
	if err != nil {
		return false, err
	}
	
	return validator.IsBonded(), nil
}

// GetAllActiveFibreProviders returns all fibre provider info for active validators
func (k Keeper) GetAllActiveFibreProviders(ctx context.Context) ([]types.ActiveFibreProvider, error) {
	bondedValidators, err := k.stakingKeeper.GetBondedValidatorsByPower(ctx)
	if err != nil {
		return nil, err
	}

	var providers []types.ActiveFibreProvider
	for _, validator := range bondedValidators {
		valAddr, err := sdk.ValAddressFromBech32(validator.OperatorAddress)
		if err != nil {
			continue
		}

		info, found := k.GetFibreProviderInfo(ctx, valAddr)
		if found {
			providers = append(providers, types.ActiveFibreProvider{
				ValidatorAddress: validator.OperatorAddress,
				Info:             &info,
			})
		}
	}

	return providers, nil
}

// IterateAllFibreProviderInfo iterates over all fibre provider info
func (k Keeper) IterateAllFibreProviderInfo(ctx context.Context, cb func(validatorAddr string, info types.FibreProviderInfo) bool) {
	store := k.getStore(ctx)
	iterator := storetypes.KVStorePrefixIterator(store, types.KeyPrefix(types.FibreProviderInfoKey))
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var info types.FibreProviderInfo
		k.cdc.MustUnmarshal(iterator.Value(), &info)
		
		// Extract validator address from key
		key := string(iterator.Key())
		validatorAddr := key[len(types.FibreProviderInfoKey):]
		
		if cb(validatorAddr, info) {
			break
		}
	}
}

// getStore returns the fibre module store
func (k Keeper) getStore(ctx context.Context) storetypes.KVStore {
	if sdkCtx, ok := ctx.(sdk.Context); ok {
		return sdkCtx.KVStore(k.storeKey)
	}
	return sdk.UnwrapSDKContext(ctx).KVStore(k.storeKey)
}