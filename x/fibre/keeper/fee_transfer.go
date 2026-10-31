package keeper

import (
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// BatchedFeeTransferVersion is the first app version that moves settled
// payments to the fee collector once per block, in EndBlock.
const BatchedFeeTransferVersion = 11

var moduleAddress = authtypes.NewModuleAddress(types.ModuleName)

// payFeeCollector routes a settled payment to the fee collector: one bank send
// per payment before BatchedFeeTransferVersion, a transient-store tally after.
func (k Keeper) payFeeCollector(ctx sdk.Context, amount sdk.Coin) error {
	if cp := ctx.ConsensusParams(); cp.GetVersion().GetApp() < BatchedFeeTransferVersion {
		if err := k.bankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, authtypes.FeeCollectorName, sdk.NewCoins(amount)); err != nil {
			return errorsmod.Wrap(err, "failed to send fibre payment to fee collector")
		}
		return nil
	}

	store := ctx.TransientStore(k.tStoreKey)
	key := types.PendingFeeTransferKey(amount.Denom)
	pending := math.ZeroInt()
	if bz := store.Get(key); bz != nil {
		if err := pending.Unmarshal(bz); err != nil {
			return err
		}
	}
	// Reject exactly when the per-payment send would have: the module balance
	// must still cover this payment after the ones deferred earlier in the block.
	balance := k.bankKeeper.GetBalance(ctx, moduleAddress, amount.Denom)
	if spendable := balance.Amount.Sub(pending); spendable.LT(amount.Amount) {
		err := errorsmod.Wrapf(sdkerrors.ErrInsufficientFunds, "spendable balance %s is smaller than %s", sdk.Coin{Denom: amount.Denom, Amount: spendable}, amount)
		return errorsmod.Wrap(err, "failed to send fibre payment to fee collector")
	}
	bz, err := pending.Add(amount.Amount).Marshal()
	if err != nil {
		return err
	}
	store.Set(key, bz)
	return nil
}

// EndBlocker sends the payments settled this block to the fee collector in one transfer.
func (k Keeper) EndBlocker(ctx sdk.Context) error {
	store := ctx.TransientStore(k.tStoreKey)
	iterator := storetypes.KVStorePrefixIterator(store, types.PendingFeeTransferKeyPrefix)
	var (
		coins sdk.Coins
		keys  [][]byte
	)
	for ; iterator.Valid(); iterator.Next() {
		amount := math.ZeroInt()
		if err := amount.Unmarshal(iterator.Value()); err != nil {
			iterator.Close()
			return err
		}
		denom := string(iterator.Key()[len(types.PendingFeeTransferKeyPrefix):])
		coins = coins.Add(sdk.NewCoin(denom, amount))
		keys = append(keys, append([]byte(nil), iterator.Key()...))
	}
	iterator.Close()
	for _, key := range keys {
		store.Delete(key)
	}
	if coins.IsZero() {
		return nil
	}
	if err := k.bankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, authtypes.FeeCollectorName, coins); err != nil {
		return errorsmod.Wrap(err, "failed to send settled fibre payments to fee collector")
	}
	return nil
}
