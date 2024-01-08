package ante_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/ante"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

func TestCheckTxFeeWithGlobalMinGasPrices(t *testing.T) {
	// try and build a tx
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	builder := encCfg.TxConfig.NewTxBuilder()
	err := builder.SetMsgs(banktypes.NewMsgSend(
		testnode.RandomAddress().(sdk.AccAddress),
		testnode.RandomAddress().(sdk.AccAddress),
		sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 10))),
	)
	require.NoError(t, err)

	feeAmount := int64(1000) // Set the desired fee amount here

	gasLimit := uint64(float64(feeAmount) / appconsts.GlobalMinGasPrice)
	builder.SetGasLimit(gasLimit)

	testCases := []struct {
		name   string
		fee    sdk.Coins
		expErr bool
	}{
		{
			name:   "bad tx; fee below required minimum",
			fee:    sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount-1)),
			expErr: true,
		},
		{
			name:   "good tx; fee above required minimum",
			fee:    sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount+1)),
			expErr: false,
		},
		{
			name:   "good tx; fee equal to required minimum",
			fee:    sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, feeAmount)),
			expErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// set the fee and gas before calling checktx
			builder.SetFeeAmount(tc.fee)
			tx := builder.GetTx()
			_, _, err := ante.CheckTxFeeWithGlobalMinGasPrices(sdk.Context{}, tx)
			if tc.expErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
