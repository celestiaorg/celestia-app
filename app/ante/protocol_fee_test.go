package ante_test

import (
	"context"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v7/app/ante"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v7/pkg/feeaddress"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"
)

// protoFeeMockFeeTx implements sdk.Tx and sdk.FeeTx for testing ProtocolFeeTerminatorDecorator.
type protoFeeMockFeeTx struct {
	msgs []sdk.Msg
	fee  sdk.Coins
	gas  uint64
}

func (m *protoFeeMockFeeTx) GetMsgs() []sdk.Msg                    { return m.msgs }
func (m *protoFeeMockFeeTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }
func (m *protoFeeMockFeeTx) ValidateBasic() error                  { return nil }
func (m *protoFeeMockFeeTx) GetFee() sdk.Coins                     { return m.fee }
func (m *protoFeeMockFeeTx) GetGas() uint64                        { return m.gas }
func (m *protoFeeMockFeeTx) FeePayer() []byte                      { return nil }
func (m *protoFeeMockFeeTx) FeeGranter() []byte                    { return nil }

// protoFeeMockBankKeeper implements feeaddress.ProtocolFeeBankKeeper for testing.
type protoFeeMockBankKeeper struct {
	sentToModule map[string]sdk.Coins
}

func (m *protoFeeMockBankKeeper) SendCoinsFromAccountToModule(_ context.Context, _ sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	if m.sentToModule == nil {
		m.sentToModule = make(map[string]sdk.Coins)
	}
	m.sentToModule[recipientModule] = amt
	return nil
}

// protoFeeMockNonFeeTx implements sdk.Tx but NOT sdk.FeeTx for testing error path.
type protoFeeMockNonFeeTx struct {
	msgs []sdk.Msg
}

func (m *protoFeeMockNonFeeTx) GetMsgs() []sdk.Msg                    { return m.msgs }
func (m *protoFeeMockNonFeeTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }
func (m *protoFeeMockNonFeeTx) ValidateBasic() error                  { return nil }

// protoFeeMockBankKeeperWithError implements feeaddress.ProtocolFeeBankKeeper and returns an error.
type protoFeeMockBankKeeperWithError struct {
	err error
}

func (m *protoFeeMockBankKeeperWithError) SendCoinsFromAccountToModule(_ context.Context, _ sdk.AccAddress, _ string, _ sdk.Coins) error {
	return m.err
}

func protoFeeNextAnteHandler(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
	return ctx, nil
}

func TestProtocolFeeTerminatorRejectsUserSubmittedTx(t *testing.T) {
	bankKeeper := &protoFeeMockBankKeeper{}
	decorator := ante.NewProtocolFeeTerminatorDecorator(bankKeeper)

	msg := feeaddress.NewMsgPayProtocolFee()
	fee := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	tx := &protoFeeMockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create CheckTx context - this simulates a user submitting the tx
	ctx := sdk.NewContext(nil, tmproto.Header{}, true, log.NewNopLogger()) // isCheckTx = true

	_, err := decorator.AnteHandle(ctx, tx, false, protoFeeNextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "MsgPayProtocolFee cannot be submitted by users")
}

func TestProtocolFeeTerminatorRejectsReCheckTx(t *testing.T) {
	bankKeeper := &protoFeeMockBankKeeper{}
	decorator := ante.NewProtocolFeeTerminatorDecorator(bankKeeper)

	msg := feeaddress.NewMsgPayProtocolFee()
	fee := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	tx := &protoFeeMockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create ReCheckTx context
	ctx := sdk.NewContext(nil, tmproto.Header{}, true, log.NewNopLogger()).WithIsReCheckTx(true)

	_, err := decorator.AnteHandle(ctx, tx, false, protoFeeNextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "MsgPayProtocolFee cannot be submitted by users")
}

func TestProtocolFeeTerminatorValidatesSingleDenom(t *testing.T) {
	bankKeeper := &protoFeeMockBankKeeper{}
	decorator := ante.NewProtocolFeeTerminatorDecorator(bankKeeper)

	msg := feeaddress.NewMsgPayProtocolFee()
	// Multiple denoms in fee - should be rejected
	fee := sdk.NewCoins(
		sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)),
		sdk.NewCoin("otherdenom", math.NewInt(500)),
	)
	tx := &protoFeeMockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create DeliverTx context (not CheckTx)
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())

	_, err := decorator.AnteHandle(ctx, tx, false, protoFeeNextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "protocol fee tx requires exactly one fee coin")
}

func TestProtocolFeeTerminatorRejectsWrongDenom(t *testing.T) {
	bankKeeper := &protoFeeMockBankKeeper{}
	decorator := ante.NewProtocolFeeTerminatorDecorator(bankKeeper)

	msg := feeaddress.NewMsgPayProtocolFee()
	// Wrong denom - should be rejected
	fee := sdk.NewCoins(sdk.NewCoin("wrongdenom", math.NewInt(1000)))
	tx := &protoFeeMockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create DeliverTx context (not CheckTx)
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())

	_, err := decorator.AnteHandle(ctx, tx, false, protoFeeNextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "protocol fee tx requires utia denom")
}

func TestProtocolFeeTerminatorSuccess(t *testing.T) {
	bankKeeper := &protoFeeMockBankKeeper{}
	decorator := ante.NewProtocolFeeTerminatorDecorator(bankKeeper)

	msg := feeaddress.NewMsgPayProtocolFee()
	fee := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	tx := &protoFeeMockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create DeliverTx context (not CheckTx)
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())

	_, err := decorator.AnteHandle(ctx, tx, false, protoFeeNextAnteHandler)

	require.NoError(t, err)
	// Verify fee was sent to fee collector
	require.Equal(t, fee, bankKeeper.sentToModule[authtypes.FeeCollectorName])
}

func TestProtocolFeeTerminatorRejectsSimulation(t *testing.T) {
	bankKeeper := &protoFeeMockBankKeeper{}
	decorator := ante.NewProtocolFeeTerminatorDecorator(bankKeeper)

	msg := feeaddress.NewMsgPayProtocolFee()
	fee := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	tx := &protoFeeMockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create DeliverTx context but pass simulate=true
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())

	_, err := decorator.AnteHandle(ctx, tx, true, protoFeeNextAnteHandler) // simulate = true

	require.Error(t, err)
	require.ErrorContains(t, err, "MsgPayProtocolFee cannot be submitted by users")
}

func TestProtocolFeeTerminatorRejectsNonFeeTx(t *testing.T) {
	bankKeeper := &protoFeeMockBankKeeper{}
	decorator := ante.NewProtocolFeeTerminatorDecorator(bankKeeper)

	msg := feeaddress.NewMsgPayProtocolFee()
	tx := &protoFeeMockNonFeeTx{msgs: []sdk.Msg{msg}}

	// Create DeliverTx context
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())

	_, err := decorator.AnteHandle(ctx, tx, false, protoFeeNextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "tx must implement FeeTx")
}

func TestProtocolFeeTerminatorBankTransferFailure(t *testing.T) {
	bankKeeper := &protoFeeMockBankKeeperWithError{err: sdkerrors.ErrInsufficientFunds}
	decorator := ante.NewProtocolFeeTerminatorDecorator(bankKeeper)

	msg := feeaddress.NewMsgPayProtocolFee()
	fee := sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000)))
	tx := &protoFeeMockFeeTx{msgs: []sdk.Msg{msg}, fee: fee, gas: 50000}

	// Create DeliverTx context (not CheckTx)
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())

	_, err := decorator.AnteHandle(ctx, tx, false, protoFeeNextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "failed to deduct fee from fee address")
}

func TestProtocolFeeTerminatorZeroFeeRejected(t *testing.T) {
	bankKeeper := &protoFeeMockBankKeeper{}
	decorator := ante.NewProtocolFeeTerminatorDecorator(bankKeeper)

	msg := feeaddress.NewMsgPayProtocolFee()
	// Zero fee should be rejected
	tx := &protoFeeMockFeeTx{msgs: []sdk.Msg{msg}, fee: sdk.Coins{}, gas: 50000}

	// Create DeliverTx context (not CheckTx)
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())

	_, err := decorator.AnteHandle(ctx, tx, false, protoFeeNextAnteHandler)

	require.Error(t, err)
	require.ErrorContains(t, err, "protocol fee tx requires exactly one fee coin")
}
