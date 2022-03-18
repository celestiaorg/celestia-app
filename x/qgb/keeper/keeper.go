package keeper

import (
	"fmt"
	"time"

	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/tendermint/tendermint/libs/log"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

type Keeper struct {
	cdc        codec.BinaryCodec
	storeKey   sdk.StoreKey
	paramSpace paramtypes.Subspace

	StakingKeeper StakingKeeper
}

func NewKeeper(cdc codec.BinaryCodec, storeKey sdk.StoreKey, paramSpace paramtypes.Subspace, stakingKeeper StakingKeeper) *Keeper {
	// set KeyTable if it has not already been set
	if !paramSpace.HasKeyTable() {
		paramSpace = paramSpace.WithKeyTable(types.ParamKeyTable())
	}

	return &Keeper{
		cdc:           cdc,
		storeKey:      storeKey,
		StakingKeeper: stakingKeeper,
		paramSpace:    paramSpace,
	}
}

// GetParams returns the parameters from the store
func (k Keeper) GetParams(ctx sdk.Context) (params types.Params) {
	k.paramSpace.GetParamSet(ctx, &params)
	return
}

// SetParams sets the parameters in the store
func (k Keeper) SetParams(ctx sdk.Context, ps types.Params) {
	k.paramSpace.SetParamSet(ctx, &ps)
}

// DeserializeValidatorIterator returns validators from the validator iterator.
// Adding here in gravity keeper as cdc is not available inside endblocker.
func (k Keeper) DeserializeValidatorIterator(vals []byte) stakingtypes.ValAddresses {
	validators := stakingtypes.ValAddresses{
		Addresses: []string{},
	}
	k.cdc.MustUnmarshal(vals, &validators)
	return validators
}

// StakingKeeper restricts the functionality of the bank keeper used in the payment keeper
type StakingKeeper interface {
	GetValidator(ctx sdk.Context, addr sdk.ValAddress) (validator stakingtypes.Validator, found bool)
	GetBondedValidatorsByPower(ctx sdk.Context) []stakingtypes.Validator
	GetLastValidatorPower(ctx sdk.Context, valAddr sdk.ValAddress) int64
	GetParams(ctx sdk.Context) stakingtypes.Params
	ValidatorQueueIterator(ctx sdk.Context, endTime time.Time, endHeight int64) sdk.Iterator
}

func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

// prefixRange turns a prefix into a (start, end) range. The start is the given prefix value and
// the end is calculated by adding 1 bit to the start value. Nil is not allowed as prefix.
// 		Example: []byte{1, 3, 4} becomes []byte{1, 3, 5}
// 				 []byte{15, 42, 255, 255} becomes []byte{15, 43, 0, 0}
//
// In case of an overflow the end is set to nil.
//		Example: []byte{255, 255, 255, 255} becomes nil
func prefixRange(prefix []byte) ([]byte, []byte) {
	if prefix == nil {
		panic("nil key not allowed")
	}
	// special case: no prefix is whole range
	if len(prefix) == 0 {
		return nil, nil
	}

	// copy the prefix and update last byte
	end := make([]byte, len(prefix))
	copy(end, prefix)
	l := len(end) - 1
	end[l]++

	for end[l] == 0 && l > 0 {
		l--
		end[l]++
	}

	// set the end as nil in case of overflow
	if l == 0 && end[0] == 0 {
		end = nil
	}
	return prefix, end
}
