package keeper

import (
	"time"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/cosmos/cosmos-sdk/codec"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

const (
	HashKey  = "hash"
	StoreKey = "ssz"
)

type Keeper struct {
	cdc        codec.BinaryCodec
	storeKey   storetypes.StoreKey
	paramSpace paramtypes.Subspace

	StakingKeeper StakingKeeper
}

func NewKeeper(storeKey storetypes.StoreKey, stakingKeeper StakingKeeper) Keeper {
	return Keeper{
		storeKey:      storeKey,
		StakingKeeper: stakingKeeper,
	}
}

type StakingKeeper interface {
	GetValidator(ctx sdk.Context, addr sdk.ValAddress) (validator stakingtypes.Validator, found bool)
	GetBondedValidatorsByPower(ctx sdk.Context) []stakingtypes.Validator
	GetLastValidatorPower(ctx sdk.Context, valAddr sdk.ValAddress) int64
	GetParams(ctx sdk.Context) stakingtypes.Params
	ValidatorQueueIterator(ctx sdk.Context, endTime time.Time, endHeight int64) sdk.Iterator
}

func (k Keeper) CurrentValsetSSZHash(ctx sdk.Context) ([]byte, error) {
	validators := k.StakingKeeper.GetBondedValidatorsByPower(ctx)
	if len(validators) == 0 {
		return nil, types.ErrNoValidators
	}

	// TODO: serialize the validator using the ssz codec
	// TODO: return the hash

	return nil, nil
}

func (k Keeper) SetSSZHash(ctx sdk.Context, hash []byte) {
	store := ctx.KVStore(k.storeKey)
	store.Set([]byte(HashKey), hash)
}
