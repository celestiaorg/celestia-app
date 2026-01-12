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
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/x/burn/types"
)

type mockBankKeeper struct {
	balances         map[string]sdk.Coins
	sentToModule     sdk.Coins
	burnedFromModule sdk.Coins
}

func newMockBankKeeper() *mockBankKeeper {
	return &mockBankKeeper{
		balances: make(map[string]sdk.Coins),
	}
}

func (m *mockBankKeeper) SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	balance := m.balances[senderAddr.String()]
	if !balance.IsAllGTE(amt) {
		return fmt.Errorf("insufficient balance: have %s, want %s", balance, amt)
	}
	m.balances[senderAddr.String()] = balance.Sub(amt...)
	m.sentToModule = amt
	return nil
}

func (m *mockBankKeeper) BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	m.burnedFromModule = amt
	return nil
}

// createTestContext creates a minimal SDK context for testing
func createTestContext(t *testing.T) sdk.Context {
	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NoOpMetrics{})
	stateStore.MountStoreWithDB(storetypes.NewKVStoreKey("test"), storetypes.StoreTypeIAVL, nil)
	require.NoError(t, stateStore.LoadLatestVersion())
	return sdk.NewContext(stateStore, tmproto.Header{}, false, log.NewNopLogger())
}

func TestBurn_Success(t *testing.T) {
	bankKeeper := newMockBankKeeper()
	signer := sdk.AccAddress("test_signer__________")
	amount := sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000))
	bankKeeper.balances[signer.String()] = sdk.NewCoins(amount)

	keeper := NewKeeper(bankKeeper)
	msg := &types.MsgBurn{
		Signer: signer.String(),
		Amount: amount,
	}

	ctx := createTestContext(t)
	resp, err := keeper.Burn(sdk.WrapSDKContext(ctx), msg)

	require.NoError(t, err)
	require.Equal(t, amount, resp.Burned)
	require.Equal(t, sdk.NewCoins(amount), bankKeeper.burnedFromModule)
}

func TestBurn_WrongDenom(t *testing.T) {
	bankKeeper := newMockBankKeeper()
	signer := sdk.AccAddress("test_signer__________")
	wrongDenom := sdk.NewCoin("wrongdenom", math.NewInt(1000))
	bankKeeper.balances[signer.String()] = sdk.NewCoins(wrongDenom)

	keeper := NewKeeper(bankKeeper)
	msg := &types.MsgBurn{
		Signer: signer.String(),
		Amount: wrongDenom,
	}

	ctx := createTestContext(t)
	_, err := keeper.Burn(sdk.WrapSDKContext(ctx), msg)

	require.Error(t, err)
	require.Contains(t, err.Error(), "only")
}

func TestBurn_InsufficientBalance(t *testing.T) {
	bankKeeper := newMockBankKeeper()
	signer := sdk.AccAddress("test_signer__________")
	amount := sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000))
	// Set balance lower than requested burn amount
	bankKeeper.balances[signer.String()] = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(500)))

	keeper := NewKeeper(bankKeeper)
	msg := &types.MsgBurn{
		Signer: signer.String(),
		Amount: amount,
	}

	ctx := createTestContext(t)
	_, err := keeper.Burn(sdk.WrapSDKContext(ctx), msg)

	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient")
}

func TestBurn_ZeroAmount(t *testing.T) {
	bankKeeper := newMockBankKeeper()
	signer := sdk.AccAddress("test_signer__________")
	zeroAmount := sdk.NewCoin(appconsts.BondDenom, math.ZeroInt())

	keeper := NewKeeper(bankKeeper)
	msg := &types.MsgBurn{
		Signer: signer.String(),
		Amount: zeroAmount,
	}

	ctx := createTestContext(t)
	_, err := keeper.Burn(sdk.WrapSDKContext(ctx), msg)

	require.Error(t, err)
	require.Contains(t, err.Error(), "positive")
}
