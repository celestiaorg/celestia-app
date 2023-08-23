package ante_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/ante"
	"github.com/celestiaorg/celestia-app/app/encoding"
	appconsts "github.com/celestiaorg/celestia-app/pkg/appconsts"
	v2 "github.com/celestiaorg/celestia-app/pkg/appconsts/v2"
	"github.com/celestiaorg/celestia-app/pkg/user"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	version "github.com/tendermint/tendermint/proto/tendermint/version"
)

func TestGasRefundAnteHandle(t *testing.T) {
	defaultCtx := sdk.Context{}.WithBlockHeader(tmproto.Header{Height: 1, Version: version.Consensus{App: v2.Version}})
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	originalSender := testfactory.GenerateAddress()
	feeGranter := testfactory.GenerateAddress()
	bankMsg := bank.NewMsgSend(originalSender, testfactory.GenerateAddress(), sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1)))
	newBuilder := func() client.TxBuilder {
		builder := enc.TxConfig.NewTxBuilder()
		_ = builder.SetMsgs(bankMsg)
		return builder
	}

	testCases := []struct {
		name             string
		ctx              func() sdk.Context
		tx               func() sdk.Tx
		recipientAddress sdk.AccAddress
		amount           uint64
		refunded         bool
	}{
		{
			name: "valid transaction",
			ctx: func() sdk.Context {
				ctx := defaultCtx.WithGasMeter(sdk.NewGasMeter(1000))
				ctx.GasMeter().ConsumeGas(200, "test")
				return ctx
			},
			tx: func() sdk.Tx {
				builder := newBuilder()
				user.SetFee(1000)(builder)
				user.SetGasLimit(1000)(builder)
				return builder.GetTx()
			},
			refunded:         true,
			recipientAddress: originalSender,
			amount:           800,
		},
		{
			name: "valid transaction with fee granter",
			ctx: func() sdk.Context {
				ctx := defaultCtx.WithGasMeter(sdk.NewGasMeter(1000))
				ctx.GasMeter().ConsumeGas(200, "test")
				return ctx
			},
			tx: func() sdk.Tx {
				builder := newBuilder()
				user.SetFee(1000)(builder)
				user.SetGasLimit(1000)(builder)
				user.SetFeeGranter(feeGranter)(builder)
				return builder.GetTx()
			},
			refunded:         true,
			recipientAddress: feeGranter,
			amount:           800,
		},
		{
			name: "transaction of an earlier version",
			ctx: func() sdk.Context {
				ctx := defaultCtx.WithGasMeter(sdk.NewGasMeter(1000))
				ctx.GasMeter().ConsumeGas(200, "test")
				ctx = ctx.WithBlockHeader(tmproto.Header{Height: 1, Version: version.Consensus{App: v2.Version - 1}})
				return ctx
			},
			tx: func() sdk.Tx {
				builder := newBuilder()
				user.SetFee(1000)(builder)
				user.SetGasLimit(1000)(builder)
				return builder.GetTx()
			},
			refunded: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			bk := newBankKeeper(t, tc.recipientAddress, tc.amount)
			decorator := ante.NewGasRefundDecorator(v2.Version, bk)
			anteHandler := sdk.ChainAnteDecorators(decorator)
			_, err := anteHandler(tc.ctx(), tc.tx(), false)
			require.NoError(t, err)
			if tc.refunded {
				require.True(t, bk.WasCalled())
			} else {
				require.False(t, bk.WasCalled())
			}
		})
	}
}

func newBankKeeper(t *testing.T, expectedRecipient sdk.AccAddress, expectedAmount uint64) *mockBankKeeper {
	return &mockBankKeeper{
		t:                 t,
		expectedRecipient: expectedRecipient,
		expectedAmount:    expectedAmount,
	}
}

type mockBankKeeper struct {
	t                 *testing.T
	expectedRecipient sdk.AccAddress
	expectedAmount    uint64
	called            bool
}

var _ ante.BankKeeper = (*mockBankKeeper)(nil)

func (m *mockBankKeeper) SendCoinsFromModuleToAccount(_ sdk.Context, _ string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
	require.Equal(m.t, m.expectedRecipient, recipientAddr)
	require.Len(m.t, amt, 1)
	amount := amt.AmountOf(appconsts.BondDenom).Uint64()
	require.Equal(m.t, m.expectedAmount, amount)
	m.called = true
	return nil
}

func (m *mockBankKeeper) WasCalled() bool {
	return m.called
}
