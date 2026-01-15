package feeaddress

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
)

const (
	// testAmount is a standard test amount in utia for unit tests.
	testAmount = 1000
)

type mockBankKeeper struct {
	balances        map[string]sdk.Coins
	sentToModule    map[string]sdk.Coins
	sendToModuleErr error
}

func newMockBankKeeper() *mockBankKeeper {
	return &mockBankKeeper{
		balances:     make(map[string]sdk.Coins),
		sentToModule: make(map[string]sdk.Coins),
	}
}

func (m *mockBankKeeper) GetBalance(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	balance := m.balances[addr.String()]
	return sdk.NewCoin(denom, balance.AmountOf(denom))
}

func (m *mockBankKeeper) SendCoinsFromAccountToModule(_ context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	if m.sendToModuleErr != nil {
		return m.sendToModuleErr
	}
	balance := m.balances[senderAddr.String()]
	m.balances[senderAddr.String()] = balance.Sub(amt...)
	m.sentToModule[recipientModule] = amt
	return nil
}

func createTestContext() sdk.Context {
	return sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())
}

// TestEndBlockerForwardsTokens verifies that the EndBlocker forwards utia tokens
// present at the fee address to the fee collector module.
func TestEndBlockerForwardsTokens(t *testing.T) {
	bankKeeper := newMockBankKeeper()
	amount := sdk.NewCoin(appconsts.BondDenom, math.NewInt(testAmount))
	bankKeeper.balances[types.FeeAddress.String()] = sdk.NewCoins(amount)

	keeper := NewKeeper(bankKeeper)
	ctx := createTestContext()

	err := keeper.EndBlocker(ctx)

	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(amount), bankKeeper.sentToModule[authtypes.FeeCollectorName])
}

// TestEndBlockerNoBalance verifies that the EndBlocker is a no-op when
// the fee address has zero balance, and no forwarding operations are performed.
func TestEndBlockerNoBalance(t *testing.T) {
	bankKeeper := newMockBankKeeper()

	keeper := NewKeeper(bankKeeper)
	ctx := createTestContext()

	err := keeper.EndBlocker(ctx)

	require.NoError(t, err)
	require.Empty(t, bankKeeper.sentToModule)
}

// TestFeeAddressQuery verifies the Query/FeeAddress gRPC endpoint returns
// the correct bech32-encoded fee address for programmatic discovery.
func TestFeeAddressQuery(t *testing.T) {
	bankKeeper := newMockBankKeeper()
	keeper := NewKeeper(bankKeeper)
	ctx := createTestContext()

	resp, err := keeper.FeeAddress(ctx, &types.QueryFeeAddressRequest{})
	require.NoError(t, err)
	require.Equal(t, types.FeeAddressBech32, resp.FeeAddress)
}

// TestEndBlockerSendToModuleFails verifies that when SendCoinsFromAccountToModule
// fails, the EndBlocker returns an error.
func TestEndBlockerSendToModuleFails(t *testing.T) {
	bankKeeper := newMockBankKeeper()
	amount := sdk.NewCoin(appconsts.BondDenom, math.NewInt(testAmount))
	bankKeeper.balances[types.FeeAddress.String()] = sdk.NewCoins(amount)
	bankKeeper.sendToModuleErr = fmt.Errorf("module account not found")

	keeper := NewKeeper(bankKeeper)
	ctx := createTestContext()

	err := keeper.EndBlocker(ctx)

	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to forward to fee collector")
}

// TestEndBlockerEmitsEvent verifies that the EndBlocker emits a typed
// EventFeeForwarded event with correct from address and amount attributes.
func TestEndBlockerEmitsEvent(t *testing.T) {
	bankKeeper := newMockBankKeeper()
	amount := sdk.NewCoin(appconsts.BondDenom, math.NewInt(testAmount))
	bankKeeper.balances[types.FeeAddress.String()] = sdk.NewCoins(amount)

	keeper := NewKeeper(bankKeeper)
	ctx := createTestContext()

	err := keeper.EndBlocker(ctx)

	require.NoError(t, err)

	// Verify event was emitted
	events := ctx.EventManager().Events()
	require.Len(t, events, 1)
	require.Equal(t, "celestia.feeaddress.v1.EventFeeForwarded", events[0].Type)

	// Verify event attributes
	var foundFrom, foundAmount bool
	for _, attr := range events[0].Attributes {
		if attr.Key == "from" {
			require.Equal(t, "\""+types.FeeAddressBech32+"\"", attr.Value)
			foundFrom = true
		}
		if attr.Key == "amount" {
			expectedAmount := fmt.Sprintf("\"%d%s\"", testAmount, appconsts.BondDenom)
			require.Equal(t, expectedAmount, attr.Value)
			foundAmount = true
		}
	}
	require.True(t, foundFrom, "from attribute not found in event")
	require.True(t, foundAmount, "amount attribute not found in event")
}
