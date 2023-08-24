package ante

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
)

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
			name:        "1 utia gas small gas",
			fee:         sdk.NewCoins(sdk.NewInt64Coin("utia", 1)),
			gas:         1,
			expectedPri: 1000000,
		},
		{
			name:        "2 utia gas price small gas",
			fee:         sdk.NewCoins(sdk.NewInt64Coin("utia", 2)),
			gas:         1,
			expectedPri: 2000000,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pri := getTxPriority(tc.fee, tc.gas)
			assert.Equal(t, tc.expectedPri, pri)
		})
	}
}
