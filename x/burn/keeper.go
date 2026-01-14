// Package burn provides functionality for permanently destroying TIA tokens.
// Tokens sent to the burn address are automatically burned at the end of each block.
package burn

import (
	"context"
	"fmt"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v7/x/burn/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Keeper handles burn operations for the burn module.
type Keeper struct {
	storeKey   storetypes.StoreKey
	bankKeeper types.BankKeeper
}

// NewKeeper creates a new Keeper instance.
func NewKeeper(storeKey storetypes.StoreKey, bankKeeper types.BankKeeper) Keeper {
	return Keeper{
		storeKey:   storeKey,
		bankKeeper: bankKeeper,
	}
}

// EndBlocker burns any utia tokens that have been sent to the burn address.
func (k Keeper) EndBlocker(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	balance := k.bankKeeper.GetBalance(sdkCtx, types.BurnAddress, appconsts.BondDenom)
	if balance.IsZero() {
		return nil
	}

	coins := sdk.NewCoins(balance)

	if err := k.bankKeeper.SendCoinsFromAccountToModule(sdkCtx, types.BurnAddress, types.ModuleName, coins); err != nil {
		return fmt.Errorf("failed to transfer to burn module: %w", err)
	}

	if err := k.bankKeeper.BurnCoins(sdkCtx, types.ModuleName, coins); err != nil {
		return fmt.Errorf("failed to burn coins: %w", err)
	}

	if err := sdkCtx.EventManager().EmitTypedEvent(types.NewBurnEvent(types.BurnAddressBech32, balance.String())); err != nil {
		return fmt.Errorf("failed to emit burn event: %w", err)
	}

	if err := k.addToTotalBurned(sdkCtx, balance); err != nil {
		return fmt.Errorf("failed to update total burned: %w", err)
	}

	return nil
}

// GetTotalBurned returns the cumulative amount of tokens burned.
// Panics if stored data is corrupted (indicates critical state corruption).
func (k Keeper) GetTotalBurned(ctx context.Context) sdk.Coin {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	store := sdkCtx.KVStore(k.storeKey)

	bz := store.Get(types.TotalBurnedKey)
	if bz == nil {
		return sdk.NewCoin(appconsts.BondDenom, math.ZeroInt())
	}

	var coin sdk.Coin
	if err := coin.Unmarshal(bz); err != nil {
		panic(fmt.Errorf("failed to unmarshal TotalBurned: %w (state corruption)", err))
	}
	return coin
}

// addToTotalBurned adds the burned amount to the cumulative total.
// Returns error if marshaling fails (should never happen for valid sdk.Coin).
func (k Keeper) addToTotalBurned(ctx sdk.Context, burned sdk.Coin) error {
	store := ctx.KVStore(k.storeKey)

	current := k.GetTotalBurned(ctx)
	updated := current.Add(burned)

	bz, err := updated.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal TotalBurned: %w", err)
	}
	store.Set(types.TotalBurnedKey, bz)
	return nil
}

// TotalBurned implements the Query/TotalBurned gRPC method.
func (k Keeper) TotalBurned(ctx context.Context, _ *types.QueryTotalBurnedRequest) (*types.QueryTotalBurnedResponse, error) {
	return &types.QueryTotalBurnedResponse{
		TotalBurned: k.GetTotalBurned(ctx),
	}, nil
}

// BurnAddress implements the Query/BurnAddress gRPC method.
// Returns the address where tokens should be sent to be burned.
func (k Keeper) BurnAddress(_ context.Context, _ *types.QueryBurnAddressRequest) (*types.QueryBurnAddressResponse, error) {
	return &types.QueryBurnAddressResponse{
		BurnAddress: types.BurnAddressBech32,
	}, nil
}
