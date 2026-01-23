package feeaddress

import (
	"testing"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"
)

func createTestContext() sdk.Context {
	return sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())
}

// TestForwardFeesReturnsSuccess verifies that the ForwardFees message handler
// returns success. The actual fee transfer and event emission happen in the
// FeeForwardTerminatorDecorator.
func TestForwardFeesReturnsSuccess(t *testing.T) {
	keeper := NewKeeper()
	ctx := createTestContext()

	msg := types.NewMsgForwardFees()
	resp, err := keeper.ForwardFees(ctx, msg)

	require.NoError(t, err)
	require.NotNil(t, resp)
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
