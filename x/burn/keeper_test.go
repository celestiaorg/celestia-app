package burn

import (
	"context"
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
	balances         map[string]sdk.Coins
	burnedFromModule sdk.Coins
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
	balance := m.balances[senderAddr.String()]
	m.balances[senderAddr.String()] = balance.Sub(amt...)
	return nil
}

func (m *mockBankKeeper) BurnCoins(_ context.Context, _ string, amt sdk.Coins) error {
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

func TestEndBlockerNoBalance(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	bankKeeper := newMockBankKeeper()

	keeper := NewKeeper(storeKey, bankKeeper)
	ctx := createTestContext(t, storeKey)

	err := keeper.EndBlocker(ctx)

	require.NoError(t, err)
	require.Nil(t, bankKeeper.burnedFromModule)
}

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

func TestBurnAddressQuery(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	bankKeeper := newMockBankKeeper()
	keeper := NewKeeper(storeKey, bankKeeper)
	ctx := createTestContext(t, storeKey)

	resp, err := keeper.BurnAddress(ctx, &types.QueryBurnAddressRequest{})
	require.NoError(t, err)
	require.Equal(t, types.BurnAddressBech32, resp.BurnAddress)
}
