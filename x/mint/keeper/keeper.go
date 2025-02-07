package keeper

import (
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v4/x/mint/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Keeper of the mint store
type Keeper struct {
	cdc              codec.BinaryCodec
	storeKey         storetypes.StoreKey
	stakingKeeper    types.StakingKeeper
	bankKeeper       types.BankKeeper
	feeCollectorName string
}

// NewKeeper creates a new mint Keeper instance.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeKey storetypes.StoreKey,
	stakingKeeper types.StakingKeeper,
	ak types.AccountKeeper,
	bankKeeper types.BankKeeper,
	feeCollectorName string,
) Keeper {
	// Ensure the mint module account has been set
	if addr := ak.GetModuleAddress(types.ModuleName); addr == nil {
		panic("the mint module account has not been set")
	}

	return Keeper{
		cdc:              cdc,
		storeKey:         storeKey,
		stakingKeeper:    stakingKeeper,
		bankKeeper:       bankKeeper,
		feeCollectorName: feeCollectorName,
	}
}

// GetMinter returns the minter.
func (k Keeper) GetMinter(ctx sdk.Context) (minter types.Minter) {
	store := ctx.KVStore(k.storeKey)
	b := store.Get(types.KeyMinter)
	if b == nil {
		panic("stored minter should not have been nil")
	}

	k.cdc.MustUnmarshal(b, &minter)
	return minter
}

// SetMinter sets the minter.
func (k Keeper) SetMinter(ctx sdk.Context, minter types.Minter) {
	store := ctx.KVStore(k.storeKey)
	b := k.cdc.MustMarshal(&minter)
	store.Set(types.KeyMinter, b)
}

// GetGenesisTime returns the genesis time.
func (k Keeper) GetGenesisTime(ctx sdk.Context) (gt types.GenesisTime) {
	store := ctx.KVStore(k.storeKey)
	b := store.Get(types.KeyGenesisTime)
	if b == nil {
		panic("stored genesis time should not have been nil")
	}

	k.cdc.MustUnmarshal(b, &gt)
	return gt
}

// SetGenesisTime sets the genesis time.
func (k Keeper) SetGenesisTime(ctx sdk.Context, gt types.GenesisTime) {
	store := ctx.KVStore(k.storeKey)
	b := k.cdc.MustMarshal(&gt)
	store.Set(types.KeyGenesisTime, b)
}

// StakingTokenSupply implements an alias call to the underlying staking keeper's
// StakingTokenSupply.
func (k Keeper) StakingTokenSupply(ctx sdk.Context) math.Int {
	// TODO not clear what to do if error is not nil as this was added in 0.52.
	n, _ := k.stakingKeeper.StakingTokenSupply(ctx)
	return n
}

// MintCoins implements an alias call to the underlying bank keeper's
// MintCoins.
func (k Keeper) MintCoins(ctx sdk.Context, newCoins sdk.Coins) error {
	if newCoins.Empty() {
		return nil
	}

	return k.bankKeeper.MintCoins(ctx, types.ModuleName, newCoins)
}

// SendCoinsToFeeCollector sends newly minted coins from the x/mint module to
// the x/auth fee collector module account.
func (k Keeper) SendCoinsToFeeCollector(ctx sdk.Context, coins sdk.Coins) error {
	return k.bankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, k.feeCollectorName, coins)
}
