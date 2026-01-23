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

// TestPayProtocolFeeReturnsSuccess verifies that the PayProtocolFee message handler
// returns success. The actual fee transfer and event emission happen in the
// ProtocolFeeTerminatorDecorator.
func TestPayProtocolFeeReturnsSuccess(t *testing.T) {
	keeper := NewKeeper()
	ctx := createTestContext()

	msg := types.NewMsgPayProtocolFee()
	resp, err := keeper.PayProtocolFee(ctx, msg)

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

// TestNewMsgPayProtocolFee verifies the constructor for MsgPayProtocolFee.
func TestNewMsgPayProtocolFee(t *testing.T) {
	msg := types.NewMsgPayProtocolFee()
	require.NotNil(t, msg)
}

// TestIsProtocolFeeMsg verifies the IsProtocolFeeMsg helper function correctly
// identifies fee forward transactions.
func TestIsProtocolFeeMsg(t *testing.T) {
	tests := []struct {
		name     string
		msgs     []sdk.Msg
		expected bool
	}{
		{
			name:     "single MsgPayProtocolFee returns message",
			msgs:     []sdk.Msg{types.NewMsgPayProtocolFee()},
			expected: true,
		},
		{
			name:     "empty messages returns nil",
			msgs:     []sdk.Msg{},
			expected: false,
		},
		{
			name:     "two messages returns nil",
			msgs:     []sdk.Msg{types.NewMsgPayProtocolFee(), types.NewMsgPayProtocolFee()},
			expected: false,
		},
		{
			name:     "non-MsgPayProtocolFee returns nil",
			msgs:     []sdk.Msg{&types.QueryFeeAddressRequest{}},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tx := &mockTx{msgs: tc.msgs}
			result := types.IsProtocolFeeMsg(tx)
			if tc.expected {
				require.NotNil(t, result)
			} else {
				require.Nil(t, result)
			}
		})
	}
}

// mockTx is a minimal mock implementation of sdk.Tx for testing IsProtocolFeeMsg.
type mockTx struct {
	msgs []sdk.Msg
}

func (m *mockTx) GetMsgs() []sdk.Msg {
	return m.msgs
}

func (m *mockTx) GetMsgsV2() ([]protov2.Message, error) {
	return nil, nil
}
