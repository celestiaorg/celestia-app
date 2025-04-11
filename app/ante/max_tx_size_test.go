package ante_test

import (
	"testing"

	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/app/ante"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
)

func TestMaxTxSizeDecorator(t *testing.T) {
	decorator := ante.NewMaxTxSizeDecorator()
	anteHandler := sdk.ChainAnteDecorators(decorator)

	testCases := []struct {
		name        string
		txSize      int
		expectError bool
		isCheckTx   []bool
	}{
		{
			name:        "good tx; under max tx size threshold",
			txSize:      appconsts.DefaultMaxTxSize - 1,
			expectError: false,
			isCheckTx:   []bool{true, false},
		},
		{
			name:        "bad tx; over max tx size threshold",
			txSize:      appconsts.DefaultMaxTxSize + 1,
			expectError: true,
			isCheckTx:   []bool{true, false},
		},
		{
			name:        "good tx; equal to max tx size threshold",
			txSize:      appconsts.DefaultMaxTxSize,
			expectError: false,
			isCheckTx:   []bool{true, false},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, isCheckTx := range tc.isCheckTx {

				ctx := sdk.NewContext(nil, tmproto.Header{}, isCheckTx, nil)

				txBytes := make([]byte, tc.txSize)

				ctx = ctx.WithTxBytes(txBytes)
				_, err := anteHandler(ctx, nil, false)
				if tc.expectError {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
			}
		})
	}
}
