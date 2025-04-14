package ante_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/ante"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/app/params"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
)

func TestPanicHandlerDecorator(t *testing.T) {
	decorator := ante.NewHandlePanicDecorator()
	anteHandler := sdk.ChainAnteDecorators(decorator, mockPanicDecorator{})
	ctx := sdk.Context{}
	enc := encoding.MakeTestConfig(app.ModuleEncodingRegisters...)
	builder := enc.TxConfig.NewTxBuilder()
	err := builder.SetMsgs(banktypes.NewMsgSend(testnode.RandomAddress().(sdk.AccAddress), testnode.RandomAddress().(sdk.AccAddress), sdk.NewCoins(sdk.NewInt64Coin(params.BondDenom, 10))))
	require.NoError(t, err)
	tx := builder.GetTx()
	defer func() {
		r := recover()
		require.NotNil(t, r)
		require.Equal(t, fmt.Sprint("mock panic", ante.FormatTx(tx)), r)
	}()
	_, _ = anteHandler(ctx, tx, false)
}

type mockPanicDecorator struct{}

func (d mockPanicDecorator) AnteHandle(_ sdk.Context, _ sdk.Tx, _ bool, _ sdk.AnteHandler) (newCtx sdk.Context, err error) {
	panic("mock panic")
}
