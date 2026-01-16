package feeaddress

import (
	"fmt"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v7/app/ante"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

const (
	// testAmount is a standard test amount in utia for unit tests.
	testAmount = 1000
)

func createTestContext() sdk.Context {
	return sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())
}

// createContextWithFeeAmount creates a test context with the fee amount set,
// simulating what the FeeForwardDecorator does.
func createContextWithFeeAmount(fee sdk.Coins) sdk.Context {
	ctx := createTestContext()
	return ctx.WithValue(ante.FeeForwardAmountContextKey{}, fee)
}

// TestForwardFeesEmitsEvent verifies that the ForwardFees message handler
// emits a typed EventFeeForwarded event with correct from address and amount.
func TestForwardFeesEmitsEvent(t *testing.T) {
	keeper := NewKeeper()

	amount := sdk.NewCoin(appconsts.BondDenom, math.NewInt(testAmount))
	fee := sdk.NewCoins(amount)
	ctx := createContextWithFeeAmount(fee)

	msg := types.NewMsgForwardFees()
	resp, err := keeper.ForwardFees(ctx, msg)

	require.NoError(t, err)
	require.NotNil(t, resp)

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

// TestForwardFeesNoFeeAmountInContext verifies that ForwardFees returns an error
// when the fee amount is not set in the context (should not happen in normal operation).
func TestForwardFeesNoFeeAmountInContext(t *testing.T) {
	keeper := NewKeeper()

	ctx := createTestContext() // No fee amount set
	msg := types.NewMsgForwardFees()

	_, err := keeper.ForwardFees(ctx, msg)

	require.Error(t, err)
	require.Contains(t, err.Error(), "fee forward amount not found in context")
}

// TestFeeAddressQuery verifies the Query/FeeAddress gRPC endpoint returns
// the correct bech32-encoded fee address for programmatic discovery.
func TestFeeAddressQuery(t *testing.T) {
	keeper := NewKeeper()
	ctx := createTestContext()

	resp, err := keeper.FeeAddress(ctx, &types.QueryFeeAddressRequest{})
	require.NoError(t, err)
	require.Equal(t, types.FeeAddressBech32, resp.FeeAddress)
}

// TestNewMsgForwardFees verifies the constructor for MsgForwardFees.
func TestNewMsgForwardFees(t *testing.T) {
	msg := types.NewMsgForwardFees()
	require.NotNil(t, msg)
}

// TestMsgForwardFeesValidateBasic verifies the ValidateBasic method of MsgForwardFees.
// Since the message has no fields, ValidateBasic always returns nil.
func TestMsgForwardFeesValidateBasic(t *testing.T) {
	msg := types.NewMsgForwardFees()
	err := msg.ValidateBasic()
	require.NoError(t, err)
}
