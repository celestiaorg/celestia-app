package ante

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
)

func TestCheckTxFeeWithGlobalMinGasPrices(t *testing.T) {
    // try and build a tx 
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	builder := encCfg.TxConfig.NewTxBuilder()
	err := builder.SetMsgs(banktypes.NewMsgSend(testnode.RandomAddress().(sdk.AccAddress), testnode.RandomAddress().(sdk.AccAddress), sdk.NewCoins(sdk.NewInt64Coin("utia", 10))))
	assert.NoError(t, err)
	tx := builder.GetTx()

	}

func TestGetTxPriority(t *testing.T) {
	cases := []struct {
		name        string
		fee         sdk.Coins
		gas         int64
		expectedPri int64
	}{
		{
			name:        "1 TIA fee large gas",
			fee:         sdk.NewCoins(sdk.NewInt64Coin("utia", 1_000_000)),
			gas:         1000000,
			expectedPri: 1000000,
		},
		{
			name:        "1 utia fee small gas",
			fee:         sdk.NewCoins(sdk.NewInt64Coin("utia", 1)),
			gas:         1,
			expectedPri: 1000000,
		},
		{
			name:        "2 utia fee small gas",
			fee:         sdk.NewCoins(sdk.NewInt64Coin("utia", 2)),
			gas:         1,
			expectedPri: 2000000,
		},
		{
			name:        "1_000_000 TIA fee normal gas tx",
			fee:         sdk.NewCoins(sdk.NewInt64Coin("utia", 1_000_000_000_000)),
			gas:         75000,
			expectedPri: 13333333333333,
		},
		{
			name:        "0.001 utia gas price",
			fee:         sdk.NewCoins(sdk.NewInt64Coin("utia", 1_000)),
			gas:         1_000_000,
			expectedPri: 1000,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pri := getTxPriority(tc.fee, tc.gas)
			assert.Equal(t, tc.expectedPri, pri)
		})
	}
}
