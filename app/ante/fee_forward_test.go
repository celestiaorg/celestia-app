package ante_test

import (
	"context"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v7/app/ante"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	feeaddresstypes "github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"
)

// mockFeeTx implements sdk.Tx and sdk.FeeTx for testing FeeForwardTerminatorDecorator.
type mockFeeTx struct {
	msgs []sdk.Msg
	fee  sdk.Coins
	gas  uint64
}

func (m *mockFeeTx) GetMsgs() []sdk.Msg                    { return m.msgs }
func (m *mockFeeTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }
func (m *mockFeeTx) ValidateBasic() error                  { return nil }
func (m *mockFeeTx) GetFee() sdk.Coins                     { return m.fee }
func (m *mockFeeTx) GetGas() uint64                        { return m.gas }
func (m *mockFeeTx) FeePayer() []byte                      { return nil }
func (m *mockFeeTx) FeeGranter() []byte                    { return nil }

// mockBankKeeper implements ante.FeeForwardBankKeeper for testing.
type mockBankKeeper struct {
	sentToModule map[string]sdk.Coins
}

func (m *mockBankKeeper) SendCoinsFromAccountToModule(_ context.Context, _ sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	if m.sentToModule == nil {
		m.sentToModule = make(map[string]sdk.Coins)
	}
	m.sentToModule[recipientModule] = amt
	return nil
}

// mockNonFeeTx implements sdk.Tx but NOT sdk.FeeTx for testing error path.
type mockNonFeeTx struct {
	msgs []sdk.Msg
}

func (m *mockNonFeeTx) GetMsgs() []sdk.Msg                    { return m.msgs }
func (m *mockNonFeeTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }
func (m *mockNonFeeTx) ValidateBasic() error                  { return nil }

// mockBankKeeperWithError implements ante.FeeForwardBankKeeper and returns an error.
type mockBankKeeperWithError struct {
	err error
}

func (m *mockBankKeeperWithError) SendCoinsFromAccountToModule(_ context.Context, _ sdk.AccAddress, _ string, _ sdk.Coins) error {
	return m.err
}

func TestFeeForwardTerminatorRejectsUserSubmittedTx(t *testing.T) {
	bankKeeper := &mockBankKeeper{}
	decorator := ante.NewFeeForwardTerminatorDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	fee := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	tx := &mockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create CheckTx context - this simulates a user submitting the tx
	ctx := sdk.NewContext(nil, tmproto.Header{}, true, log.NewNopLogger()) // isCheckTx = true

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "MsgForwardFees cannot be submitted by users")
}

func TestFeeForwardTerminatorRejectsReCheckTx(t *testing.T) {
	bankKeeper := &mockBankKeeper{}
	decorator := ante.NewFeeForwardTerminatorDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	fee := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	tx := &mockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create ReCheckTx context
	ctx := sdk.NewContext(nil, tmproto.Header{}, true, log.NewNopLogger()).WithIsReCheckTx(true)

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "MsgForwardFees cannot be submitted by users")
}

func TestFeeForwardTerminatorValidatesSingleDenom(t *testing.T) {
	bankKeeper := &mockBankKeeper{}
	decorator := ante.NewFeeForwardTerminatorDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	// Multiple denoms in fee - should be rejected
	fee := sdk.NewCoins(
		sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)),
		sdk.NewCoin("otherdenom", math.NewInt(500)),
	)
	tx := &mockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create DeliverTx context (not CheckTx)
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "fee forward tx requires exactly one fee coin")
}

func TestFeeForwardTerminatorRejectsWrongDenom(t *testing.T) {
	bankKeeper := &mockBankKeeper{}
	decorator := ante.NewFeeForwardTerminatorDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	// Wrong denom - should be rejected
	fee := sdk.NewCoins(sdk.NewCoin("wrongdenom", math.NewInt(1000)))
	tx := &mockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create DeliverTx context (not CheckTx)
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "fee forward tx requires utia denom")
}

func TestFeeForwardTerminatorSuccess(t *testing.T) {
	bankKeeper := &mockBankKeeper{}
	decorator := ante.NewFeeForwardTerminatorDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	fee := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	tx := &mockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create DeliverTx context (not CheckTx)
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())

	newCtx, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.NoError(t, err)
	// Verify fee was sent to fee collector
	require.Equal(t, fee, bankKeeper.sentToModule[authtypes.FeeCollectorName])
	// Verify GetFeeForwardAmount returns the correct fee
	feeFromCtx, ok := feeaddresstypes.GetFeeForwardAmount(newCtx)
	require.True(t, ok, "GetFeeForwardAmount should return fee")
	require.Equal(t, fee, feeFromCtx, "fee from context should match tx fee")
}

// TestFeeForwardTerminatorRejectsSimulation verifies that the FeeForwardTerminatorDecorator
// rejects fee forward transactions in simulation mode.
func TestFeeForwardTerminatorRejectsSimulation(t *testing.T) {
	bankKeeper := &mockBankKeeper{}
	decorator := ante.NewFeeForwardTerminatorDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	fee := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	tx := &mockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create DeliverTx context but pass simulate=true
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())

	_, err := decorator.AnteHandle(ctx, tx, true, nextAnteHandler) // simulate = true

	require.Error(t, err)
	require.ErrorContains(t, err, "MsgForwardFees cannot be submitted by users")
}

// TestFeeForwardTerminatorRejectsNonFeeTx verifies that the FeeForwardTerminatorDecorator
// rejects transactions that don't implement sdk.FeeTx.
func TestFeeForwardTerminatorRejectsNonFeeTx(t *testing.T) {
	bankKeeper := &mockBankKeeper{}
	decorator := ante.NewFeeForwardTerminatorDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	tx := &mockNonFeeTx{msgs: []sdk.Msg{msg}}

	// Create DeliverTx context
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "tx must implement FeeTx")
}

func TestFeeForwardTerminatorBankTransferFailure(t *testing.T) {
	// Test that bank transfer failure is properly handled
	bankKeeper := &mockBankKeeperWithError{err: sdkerrors.ErrInsufficientFunds}
	decorator := ante.NewFeeForwardTerminatorDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	fee := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	tx := &mockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create DeliverTx context (not CheckTx)
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "failed to deduct fee from fee address")
}

func TestFeeForwardTerminatorZeroFeeRejected(t *testing.T) {
	bankKeeper := &mockBankKeeper{}
	decorator := ante.NewFeeForwardTerminatorDecorator(bankKeeper)

	msg := feeaddresstypes.NewMsgForwardFees()
	// Zero fee should be rejected
	tx := &mockFeeTx{msgs: []sdk.Msg{msg}, fee: sdk.Coins{}, gas: 50000}

	// Create DeliverTx context (not CheckTx)
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "fee forward tx requires exactly one fee coin")
}
