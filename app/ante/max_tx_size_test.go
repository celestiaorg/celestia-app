package ante_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v3/app/ante"
	v2 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v2"
	v3 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v3"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	version "github.com/tendermint/tendermint/proto/tendermint/version"
)

func TestMaxTxSizeDecorator(t *testing.T) {
	decorator := ante.NewMaxTxSizeDecorator()
	anteHandler := sdk.ChainAnteDecorators(decorator)

	testCases := []struct {
		name        string
		txSize      int
		expectError bool
		appVersion  uint64
	}{
		{
			name:        "good tx; under max tx bytes threshold",
			txSize:      v3.MaxTxBytes - 1,
			appVersion:  v3.Version,
			expectError: false,
		},
		{
			name:        "bad tx; over max tx bytes threshold",
			txSize:      v3.MaxTxBytes + 1,
			appVersion:  v3.Version,
			expectError: true,
		},
		{
			name:        "good tx; equal to max tx bytes threshold",
			txSize:      v3.MaxTxBytes,
			appVersion:  v3.Version,
			expectError: false,
		},
		{
			name:        "good tx; limit only applies to v3 and above",
			txSize:      v3.MaxTxBytes + 10,
			appVersion:  v2.Version,
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := sdk.NewContext(nil, tmproto.Header{
				Version: version.Consensus{
					App: tc.appVersion,
				},
			}, false, nil)

			txBytes := make([]byte, tc.txSize)

			ctx = ctx.WithTxBytes(txBytes)
			_, err := anteHandler(ctx, nil, false)
			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), sdkerrors.ErrTxTooLarge.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}
