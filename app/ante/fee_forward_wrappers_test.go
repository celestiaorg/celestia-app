package ante_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v7/app/ante"
	feeaddresstypes "github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"
)

// wrapperTestTx is a minimal mock transaction for testing wrappers.
type wrapperTestTx struct {
	msgs []sdk.Msg
}

func (m wrapperTestTx) GetMsgs() []sdk.Msg                    { return m.msgs }
func (m wrapperTestTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }
func (m wrapperTestTx) ValidateBasic() error                  { return nil }

// mockInnerDecorator tracks whether it was called.
type mockInnerDecorator struct {
	called bool
}

func (m *mockInnerDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	m.called = true
	return next(ctx, tx, simulate)
}

func TestEarlyFeeForwardDetector(t *testing.T) {
	detector := ante.NewEarlyFeeForwardDetector()

	testCases := []struct {
		name          string
		tx            sdk.Tx
		expectFlagSet bool
	}{
		{
			name:          "sets flag for MsgForwardFees",
			tx:            wrapperTestTx{msgs: []sdk.Msg{feeaddresstypes.NewMsgForwardFees()}},
			expectFlagSet: true,
		},
		{
			name: "does not set flag for regular tx",
			tx: wrapperTestTx{msgs: []sdk.Msg{&banktypes.MsgSend{
				FromAddress: "cosmos1...",
				ToAddress:   "cosmos1...",
			}}},
			expectFlagSet: false,
		},
		{
			name:          "does not set flag for empty tx",
			tx:            wrapperTestTx{msgs: []sdk.Msg{}},
			expectFlagSet: false,
		},
		{
			name: "does not set flag for tx with multiple messages including MsgForwardFees",
			tx: wrapperTestTx{msgs: []sdk.Msg{
				feeaddresstypes.NewMsgForwardFees(),
				&banktypes.MsgSend{FromAddress: "cosmos1...", ToAddress: "cosmos1..."},
			}},
			expectFlagSet: false, // IsFeeForwardMsg requires exactly 1 message
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := sdk.NewContext(nil, tmproto.Header{}, false, nil)

			// Create a next handler that captures the context
			var capturedCtx sdk.Context
			nextHandler := func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
				capturedCtx = ctx
				return ctx, nil
			}

			_, err := detector.AnteHandle(ctx, tc.tx, false, nextHandler)
			require.NoError(t, err)

			// Check if the flag was set
			flagSet := feeaddresstypes.IsFeeForwardTx(capturedCtx)
			require.Equal(t, tc.expectFlagSet, flagSet, "IsFeeForwardTx should return %v", tc.expectFlagSet)
		})
	}
}

func TestSkipForFeeForwardDecorator(t *testing.T) {
	testCases := []struct {
		name              string
		isFeeForwardTx    bool
		expectInnerCalled bool
	}{
		{
			name:              "skips inner decorator for fee forward tx",
			isFeeForwardTx:    true,
			expectInnerCalled: false,
		},
		{
			name:              "calls inner decorator for regular tx",
			isFeeForwardTx:    false,
			expectInnerCalled: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			inner := &mockInnerDecorator{}
			wrapper := ante.NewSkipForFeeForwardDecorator(inner)

			ctx := sdk.NewContext(nil, tmproto.Header{}, false, nil)
			if tc.isFeeForwardTx {
				ctx = ctx.WithValue(feeaddresstypes.FeeForwardContextKey{}, true)
			}

			tx := wrapperTestTx{msgs: []sdk.Msg{}}
			nextHandler := func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
				return ctx, nil
			}

			_, err := wrapper.AnteHandle(ctx, tx, false, nextHandler)
			require.NoError(t, err)
			require.Equal(t, tc.expectInnerCalled, inner.called, "inner decorator called should be %v", tc.expectInnerCalled)
		})
	}
}

func TestIsFeeForwardTx(t *testing.T) {
	testCases := []struct {
		name     string
		ctxValue any
		expected bool
	}{
		{
			name:     "returns true when flag is set to true",
			ctxValue: true,
			expected: true,
		},
		{
			name:     "returns false when flag is set to false",
			ctxValue: false,
			expected: false,
		},
		{
			name:     "returns false when flag is not set",
			ctxValue: nil,
			expected: false,
		},
		{
			name:     "returns false when flag is wrong type",
			ctxValue: "true", // string instead of bool
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := sdk.NewContext(nil, tmproto.Header{}, false, nil)
			if tc.ctxValue != nil {
				ctx = ctx.WithValue(feeaddresstypes.FeeForwardContextKey{}, tc.ctxValue)
			}

			result := feeaddresstypes.IsFeeForwardTx(ctx)
			require.Equal(t, tc.expected, result)
		})
	}
}
