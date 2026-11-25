package types_test

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v11/x/minfee/types"
	"github.com/stretchr/testify/require"
)

func TestValidateGenesis(t *testing.T) {
	validPrice := sdkmath.LegacyMustNewDecFromStr("0.0005")

	tests := []struct {
		name    string
		genesis *types.GenesisState
		wantErr string
	}{
		{
			name:    "default genesis is valid",
			genesis: types.DefaultGenesis(),
		},
		{
			name: "missing deprecated network min gas price is rejected, not panicked",
			// NetworkMinGasPrice is a non-nullable LegacyDec. When a genesis
			// omits it the field stays at its zero value, whose internal
			// big.Int is nil. IsNegative and IsZero dereference that pointer,
			// so the previous code panicked instead of returning an error.
			genesis: &types.GenesisState{
				Params: types.Params{NetworkMinGasPrice: validPrice},
			},
			wantErr: "network min gas price",
		},
		{
			name: "zero deprecated network min gas price is rejected",
			genesis: &types.GenesisState{
				NetworkMinGasPrice: sdkmath.LegacyZeroDec(),
				Params:             types.Params{NetworkMinGasPrice: validPrice},
			},
			wantErr: "network min gas price",
		},
		{
			name: "negative deprecated network min gas price is rejected",
			genesis: &types.GenesisState{
				NetworkMinGasPrice: sdkmath.LegacyMustNewDecFromStr("-1"),
				Params:             types.Params{NetworkMinGasPrice: validPrice},
			},
			wantErr: "network min gas price",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Guard against a regression to the nil-dereference panic.
			require.NotPanics(t, func() {
				err := types.ValidateGenesis(tt.genesis)
				if tt.wantErr == "" {
					require.NoError(t, err)
					return
				}
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
			})
		})
	}
}
