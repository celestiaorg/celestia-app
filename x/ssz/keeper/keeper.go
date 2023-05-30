package keeper

import (
	"time"

	"cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

const (
	HashKey  = "hash"
	StoreKey = "ssz"
)

type Keeper struct {
	storeKey      storetypes.StoreKey
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

	sszValidators := make([]*ValidatorSSZ, len(validators))

	// Taken from z/qgb/keeper/keeper_valset.go
	for i, validator := range validators {
		val := validator.GetOperator()
		pubkey, _ := validator.ConsPubKey()
		if err := sdk.VerifyAddressFormat(val); err != nil {
			return nil, errors.Wrap(err, types.ErrInvalidValAddress.Error())
		}

		power := uint64(k.StakingKeeper.GetLastValidatorPower(ctx, val))
		sszValidators[i] = &ValidatorSSZ{
			PubKey:      pubkey.Bytes(),
			VotingPower: power,
		}
	}

	sszStruct := ValidatorSetSSZ{
		Validators: sszValidators,
	}

	root, err := sszStruct.HashTreeRoot()
	return root[:], err
}

func (k Keeper) SetSSZHash(ctx sdk.Context, hash []byte) {
	store := ctx.KVStore(k.storeKey)
	store.Set([]byte(HashKey), hash)
}

// Gets the latest SSZ hash
func (k Keeper) GetSSZHash(ctx sdk.Context) []byte {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get([]byte(HashKey))
	return bz
}
