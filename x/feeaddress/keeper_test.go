package feeaddress

import (
	"fmt"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"
)

const (
	// testAmountUtia is a standard test amount in utia (the smallest unit) for unit tests.
	testAmountUtia = 1000
)

func createTestContext() sdk.Context {
	return sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())
}

// createContextWithFeeAmount creates a test context with the fee amount set,
// simulating what the FeeForwardTerminatorDecorator does.
func createContextWithFeeAmount(fee sdk.Coins) sdk.Context {
	ctx := createTestContext()
	return ctx.WithValue(types.FeeForwardAmountContextKey{}, fee)
}

// TestForwardFeesEmitsEvent verifies that the ForwardFees message handler
// emits a typed EventFeeForwarded event with correct from address and amount.
func TestForwardFeesEmitsEvent(t *testing.T) {
	keeper := NewKeeper()

	amount := sdk.NewCoin(appconsts.BondDenom, math.NewInt(testAmountUtia))
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
	var foundFromAddress, foundAmount bool
	for _, attr := range events[0].Attributes {
		if attr.Key == "from_address" {
			require.Equal(t, "\""+types.FeeAddressBech32+"\"", attr.Value)
			foundFromAddress = true
		}
		if attr.Key == "amount" {
			expectedAmount := fmt.Sprintf("\"%d%s\"", testAmountUtia, appconsts.BondDenom)
			require.Equal(t, expectedAmount, attr.Value)
			foundAmount = true
		}
	}
	require.True(t, foundFromAddress, "from_address attribute not found in event")
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

// TestIsFeeForwardMsg verifies the IsFeeForwardMsg helper function correctly
// identifies fee forward transactions.
func TestIsFeeForwardMsg(t *testing.T) {
	tests := []struct {
		name     string
		msgs     []sdk.Msg
		expected bool
	}{
		{
			name:     "single MsgForwardFees returns message",
			msgs:     []sdk.Msg{types.NewMsgForwardFees()},
			expected: true,
		},
		{
			name:     "empty messages returns nil",
			msgs:     []sdk.Msg{},
			expected: false,
		},
		{
			name:     "two messages returns nil",
			msgs:     []sdk.Msg{types.NewMsgForwardFees(), types.NewMsgForwardFees()},
			expected: false,
		},
		{
			name:     "non-MsgForwardFees returns nil",
			msgs:     []sdk.Msg{&types.QueryFeeAddressRequest{}},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tx := &mockTx{msgs: tc.msgs}
			result := types.IsFeeForwardMsg(tx)
			if tc.expected {
				require.NotNil(t, result)
			} else {
				require.Nil(t, result)
			}
		})
	}
}

// TestGetFeeForwardAmount verifies the GetFeeForwardAmount context helper.
func TestGetFeeForwardAmount(t *testing.T) {
	testFee := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(testAmountUtia)))

	tests := []struct {
		name        string
		ctx         sdk.Context
		expectedFee sdk.Coins
		expectedOk  bool
	}{
		{
			name:        "context with fee amount returns fee",
			ctx:         createContextWithFeeAmount(testFee),
			expectedFee: testFee,
			expectedOk:  true,
		},
		{
			name:        "context without fee amount returns nil, false",
			ctx:         createTestContext(),
			expectedFee: nil,
			expectedOk:  false,
		},
		{
			name:        "context with wrong type returns nil, false",
			ctx:         createTestContext().WithValue(types.FeeForwardAmountContextKey{}, "not coins"),
			expectedFee: nil,
			expectedOk:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fee, ok := types.GetFeeForwardAmount(tc.ctx)
			require.Equal(t, tc.expectedOk, ok)
			if tc.expectedOk {
				require.True(t, fee.Equal(tc.expectedFee))
			} else {
				require.Nil(t, fee)
			}
		})
	}
}

// mockTx is a minimal mock implementation of sdk.Tx for testing IsFeeForwardMsg.
type mockTx struct {
	msgs []sdk.Msg
}

func (m *mockTx) GetMsgs() []sdk.Msg {
	return m.msgs
}

func (m *mockTx) GetMsgsV2() ([]protov2.Message, error) {
	return nil, nil
}
