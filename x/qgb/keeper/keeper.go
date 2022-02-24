package keeper

import (
	"fmt"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/tendermint/tendermint/libs/log"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type Keeper struct {
	cdc           codec.BinaryCodec
	storeKey      sdk.StoreKey
	stakingKeeper StakingKeeper
}

func NewKeeper(cdc codec.BinaryCodec, storeKey sdk.StoreKey, stakingKeeper StakingKeeper) *Keeper {
	return &Keeper{
		cdc:           cdc,
		storeKey:      storeKey,
		stakingKeeper: stakingKeeper,
	}
}

// StakingKeeper restricts the functionality of the bank keeper used in the payment keeper
type StakingKeeper interface {
	GetValidator(ctx sdk.Context, addr sdk.ValAddress) (validator stakingtypes.Validator, found bool)
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

// GetOrchestratorValidator returns the validator key associated with an account address
func (k Keeper) GetOrchestratorValidator(ctx sdk.Context, acc sdk.AccAddress) (validator stakingtypes.Validator, found bool) {
	if err := sdk.VerifyAddressFormat(acc); err != nil {
		ctx.Logger().Error("invalid validator address")
		return validator, false
	}
	store := ctx.KVStore(k.storeKey)
	valAddr := store.Get([]byte(types.GetOrchestratorAddressKey(acc)))

	if valAddr == nil {
		return stakingtypes.Validator{}, false
	}
	validator, found = k.stakingKeeper.GetValidator(ctx, valAddr)
	if !found {
		return stakingtypes.Validator{}, false
	}
	return validator, true
}
