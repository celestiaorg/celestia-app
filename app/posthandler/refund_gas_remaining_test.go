package posthandler_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/app/posthandler"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	feegrantkeeper "github.com/cosmos/cosmos-sdk/x/feegrant/keeper"
)

func TestAnteHandler(t *testing.T) {
	gasLimit := uint64(1e6)
	fee := int64(1e6)
	feePayer, err := sdk.AccAddressFromBech32("celestia1yp95ns7exf4l9jgh4rm58lmk3s6j80zylv3up8")
	assert.NoError(t, err)

	type testCase struct {
		name     string
		ctx      sdk.Context
		tx       sdk.Tx
		simulate bool
		next     sdk.AnteHandler
		wantErr  bool
		wantCtx  sdk.Context
	}
	testCases := []testCase{
		{
			name:     "want an error if transaction is not a fee tx",
			ctx:      mockContext(gasLimit),
			tx:       notFeeTx{},
			simulate: false,
			next:     mockAnteHandler(),
			wantErr:  true,
		},
		{
			name:     "want no error and no gas meter modifications if simulation is true",
			ctx:      mockContext(gasLimit),
			tx:       mockTx(gasLimit, fee, feePayer),
			simulate: true,
			next:     mockAnteHandler(),
			wantErr:  false,
			wantCtx:  mockContext(gasLimit),
		},
		{
			name:     "want gas meter to decrease if simulation is false",
			ctx:      mockContext(gasLimit),
			tx:       mockTx(gasLimit, fee, feePayer),
			simulate: false,
			next:     mockAnteHandler(),
			wantErr:  false,
			wantCtx:  contextWithRefundGasConsumed(gasLimit),
		},
	}
	ak := mockAccountKeeper()
	bk := mockBankKeeper()
	fk := mockFeeGrantKeeper()
	decorator := posthandler.NewRefundGasRemainingDecorator(ak, bk, fk)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotCtx, err := decorator.AnteHandle(tc.ctx, tc.tx, tc.simulate, tc.next)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.Equal(t, tc.wantCtx, gotCtx)
		})
	}
}

func mockContext(gasLimit uint64) sdk.Context {
	return sdk.Context{}.WithGasMeter(sdk.NewGasMeter(gasLimit))
}

func contextWithRefundGasConsumed(gasLimit uint64) sdk.Context {
	meter := sdk.NewGasMeter(gasLimit)
	meter.ConsumeGas(posthandler.RefundGasCost, "refund gas cost")
	return sdk.Context{}.WithGasMeter(meter)
}

func mockTx(gasLimit uint64, fee int64, feePayer sdk.AccAddress) sdk.Tx {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	builder := encCfg.TxConfig.NewTxBuilder()
	builder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(fee))))
	builder.SetGasLimit(gasLimit)
	builder.SetFeePayer(feePayer)

	return builder.GetTx()
}

func mockAnteHandler() sdk.AnteHandler {
	anteHandler := func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		return ctx, nil
	}
	return anteHandler
}

func mockAccountKeeper() authkeeper.AccountKeeper {
	return authkeeper.AccountKeeper{}
}

func mockBankKeeper() authtypes.BankKeeper {
	return bankkeeper.BaseKeeper{}
}

func mockFeeGrantKeeper() feegrantkeeper.Keeper {
	return feegrantkeeper.Keeper{}
}

type notFeeTx struct{}

// notFeeTx implements the sdk.Tx interface but does not implement the sdk.FeeTx interface.
var _ sdk.Tx = notFeeTx{}

func (tx notFeeTx) GetMsgs() []sdk.Msg {
	return []sdk.Msg{}
}

func (tx notFeeTx) ValidateBasic() error {
	return nil
}
