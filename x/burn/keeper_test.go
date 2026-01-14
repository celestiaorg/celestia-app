package burn

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v7/x/burn/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

type mockBankKeeper struct {
	balances               map[string]sdk.Coins
	burnedFromModule       sdk.Coins
	sendToModuleErr        error
	burnCoinsErr           error
}

func newMockBankKeeper() *mockBankKeeper {
	return &mockBankKeeper{
		balances: make(map[string]sdk.Coins),
	}
}

func (m *mockBankKeeper) GetBalance(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	balance := m.balances[addr.String()]
	return sdk.NewCoin(denom, balance.AmountOf(denom))
}

func (m *mockBankKeeper) SendCoinsFromAccountToModule(_ context.Context, senderAddr sdk.AccAddress, _ string, amt sdk.Coins) error {
	if m.sendToModuleErr != nil {
		return m.sendToModuleErr
	}
	balance := m.balances[senderAddr.String()]
	m.balances[senderAddr.String()] = balance.Sub(amt...)
	return nil
}

func (m *mockBankKeeper) BurnCoins(_ context.Context, _ string, amt sdk.Coins) error {
	if m.burnCoinsErr != nil {
		return m.burnCoinsErr
	}
	m.burnedFromModule = amt
	return nil
}

func createTestContext(t *testing.T, storeKey storetypes.StoreKey) sdk.Context {
	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NoOpMetrics{})
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, nil)
	require.NoError(t, stateStore.LoadLatestVersion())
	return sdk.NewContext(stateStore, tmproto.Header{}, false, log.NewNopLogger())
}

// TestEndBlockerBurnsTokens verifies that the EndBlocker burns utia tokens
// present at the burn address and updates the TotalBurned state accordingly.
func TestEndBlockerBurnsTokens(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	bankKeeper := newMockBankKeeper()
	amount := sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000))
	bankKeeper.balances[types.BurnAddress.String()] = sdk.NewCoins(amount)

	keeper := NewKeeper(storeKey, bankKeeper)
	ctx := createTestContext(t, storeKey)

	err := keeper.EndBlocker(ctx)

	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(amount), bankKeeper.burnedFromModule)
	require.Equal(t, amount, keeper.GetTotalBurned(ctx))
}

// TestEndBlockerNoBalance verifies that the EndBlocker is a no-op when
// the burn address has zero balance, and no burn operations are performed.
func TestEndBlockerNoBalance(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	bankKeeper := newMockBankKeeper()

	keeper := NewKeeper(storeKey, bankKeeper)
	ctx := createTestContext(t, storeKey)

	err := keeper.EndBlocker(ctx)

	require.NoError(t, err)
	require.Nil(t, bankKeeper.burnedFromModule)
}

// TestTotalBurnedAccumulates verifies that the TotalBurned state correctly
// accumulates across multiple EndBlocker executions (multiple blocks).
func TestTotalBurnedAccumulates(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	bankKeeper := newMockBankKeeper()
	keeper := NewKeeper(storeKey, bankKeeper)
	ctx := createTestContext(t, storeKey)

	// First burn
	amount1 := sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000))
	bankKeeper.balances[types.BurnAddress.String()] = sdk.NewCoins(amount1)
	require.NoError(t, keeper.EndBlocker(ctx))

	// Second burn
	amount2 := sdk.NewCoin(appconsts.BondDenom, math.NewInt(500))
	bankKeeper.balances[types.BurnAddress.String()] = sdk.NewCoins(amount2)
	require.NoError(t, keeper.EndBlocker(ctx))

	// Verify total
	expected := sdk.NewCoin(appconsts.BondDenom, math.NewInt(1500))
	require.Equal(t, expected, keeper.GetTotalBurned(ctx))
}

// TestTotalBurnedQuery verifies the Query/TotalBurned gRPC endpoint returns
// zero initially and the correct cumulative amount after burns occur.
func TestTotalBurnedQuery(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	bankKeeper := newMockBankKeeper()
	keeper := NewKeeper(storeKey, bankKeeper)
	ctx := createTestContext(t, storeKey)

	// Initial query returns zero
	resp, err := keeper.TotalBurned(ctx, &types.QueryTotalBurnedRequest{})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoin(appconsts.BondDenom, math.ZeroInt()), resp.TotalBurned)

	// After burn
	amount := sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000))
	bankKeeper.balances[types.BurnAddress.String()] = sdk.NewCoins(amount)
	require.NoError(t, keeper.EndBlocker(ctx))

	resp, err = keeper.TotalBurned(ctx, &types.QueryTotalBurnedRequest{})
	require.NoError(t, err)
	require.Equal(t, amount, resp.TotalBurned)
}

// TestBurnAddressQuery verifies the Query/BurnAddress gRPC endpoint returns
// the correct bech32-encoded burn address for programmatic discovery.
func TestBurnAddressQuery(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	bankKeeper := newMockBankKeeper()
	keeper := NewKeeper(storeKey, bankKeeper)
	ctx := createTestContext(t, storeKey)

	resp, err := keeper.BurnAddress(ctx, &types.QueryBurnAddressRequest{})
	require.NoError(t, err)
	require.Equal(t, types.BurnAddressBech32, resp.BurnAddress)
}

// TestEndBlockerSendToModuleFails verifies that when SendCoinsFromAccountToModule
// fails, the EndBlocker returns an error and TotalBurned is not updated.
func TestEndBlockerSendToModuleFails(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	bankKeeper := newMockBankKeeper()
	amount := sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000))
	bankKeeper.balances[types.BurnAddress.String()] = sdk.NewCoins(amount)
	bankKeeper.sendToModuleErr = fmt.Errorf("module account not found")

	keeper := NewKeeper(storeKey, bankKeeper)
	ctx := createTestContext(t, storeKey)

	err := keeper.EndBlocker(ctx)

	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to transfer to burn module")
	// TotalBurned should not be updated on error
	require.Equal(t, sdk.NewCoin(appconsts.BondDenom, math.ZeroInt()), keeper.GetTotalBurned(ctx))
}

// TestEndBlockerBurnCoinsFails verifies that when BurnCoins fails,
// the EndBlocker returns an error and TotalBurned is not updated.
func TestEndBlockerBurnCoinsFails(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	bankKeeper := newMockBankKeeper()
	amount := sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000))
	bankKeeper.balances[types.BurnAddress.String()] = sdk.NewCoins(amount)
	bankKeeper.burnCoinsErr = fmt.Errorf("insufficient funds")

	keeper := NewKeeper(storeKey, bankKeeper)
	ctx := createTestContext(t, storeKey)

	err := keeper.EndBlocker(ctx)

	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to burn coins")
	// TotalBurned should not be updated on error
	require.Equal(t, sdk.NewCoin(appconsts.BondDenom, math.ZeroInt()), keeper.GetTotalBurned(ctx))
}

// TestEndBlockerEmitsEvent verifies that the EndBlocker emits a typed
// EventBurn event with correct burner address and amount attributes.
func TestEndBlockerEmitsEvent(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	bankKeeper := newMockBankKeeper()
	amount := sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000))
	bankKeeper.balances[types.BurnAddress.String()] = sdk.NewCoins(amount)

	keeper := NewKeeper(storeKey, bankKeeper)
	ctx := createTestContext(t, storeKey)

	err := keeper.EndBlocker(ctx)

	require.NoError(t, err)

	// Verify event was emitted
	events := ctx.EventManager().Events()
	require.Len(t, events, 1)
	require.Equal(t, "celestia.burn.v1.EventBurn", events[0].Type)

	// Verify event attributes
	var foundBurner, foundAmount bool
	for _, attr := range events[0].Attributes {
		if attr.Key == "burner" {
			require.Equal(t, "\""+types.BurnAddressBech32+"\"", attr.Value)
			foundBurner = true
		}
		if attr.Key == "amount" {
			require.Equal(t, "\"1000utia\"", attr.Value)
			foundAmount = true
		}
	}
	require.True(t, foundBurner, "burner attribute not found in event")
	require.True(t, foundAmount, "amount attribute not found in event")
}
