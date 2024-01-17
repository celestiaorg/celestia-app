package ante_test

import (
	"math"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/ante"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	version "github.com/tendermint/tendermint/proto/tendermint/version"
)

func TestCheckTxFeeWithGlobalMinGasPrices(t *testing.T) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	builder := encCfg.TxConfig.NewTxBuilder()
	err := builder.SetMsgs(banktypes.NewMsgSend(
		testnode.RandomAddress().(sdk.AccAddress),
		testnode.RandomAddress().(sdk.AccAddress),
		sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 10))),
	)
	require.NoError(t, err)

	feeAmount := int64(1000)

	ctx := sdk.Context{}

	testCases := []struct {
		name       string
		fee        sdk.Coins
		gasLimit   uint64
		appVersion uint64
		expErr     bool
	}{
		{
			name:       "bad tx; fee below required minimum",
			fee:        sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount-1)),
			gasLimit:   uint64(float64(feeAmount) / appconsts.GlobalMinGasPrice),
			appVersion: uint64(2),
			expErr:     true,
		},
		{
			name:       "good tx; fee equal to required minimum",
			fee:        sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount)),
			gasLimit:   uint64(float64(feeAmount) / appconsts.GlobalMinGasPrice),
			appVersion: uint64(2),
			expErr:     false,
		},
		{
			name:       "good tx; fee above required minimum",
			fee:        sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount+1)),
			gasLimit:   uint64(float64(feeAmount) / appconsts.GlobalMinGasPrice),
			appVersion: uint64(2),
			expErr:     false,
		},
		{
			name:       "good tx; with no fee (v1)",
			fee:        sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount)),
			gasLimit:   uint64(float64(feeAmount) / appconsts.GlobalMinGasPrice),
			appVersion: uint64(1),
			expErr:     false,
		},
		{
			name:       "good tx; gas limit and fee are maximum values",
			fee:        sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, math.MaxInt64)),
			gasLimit:   math.MaxUint64,
			appVersion: uint64(2),
			expErr:     false,
		},
		{
			name:       "bad tx; gas limit and fee are 0",
			fee:        sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 0)),
			gasLimit:   0,
			appVersion: uint64(2),
			expErr:     false,
		},
		{
			name:       "good tx; minFee = 0.8, rounds up to 1",
			fee:        sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount)),
			gasLimit:   400,
			appVersion: uint64(2),
			expErr:     false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			builder.SetGasLimit(tc.gasLimit)
			builder.SetFeeAmount(tc.fee)
			tx := builder.GetTx()

			ctx = ctx.WithBlockHeader(tmproto.Header{
				Version: version.Consensus{
					App: tc.appVersion,
				},
			})
			_, _, err := ante.CheckTxFeeWithGlobalMinGasPrices(ctx, tx)
			if tc.expErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
