package types

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestValidateProtocolFee(t *testing.T) {
	validCoin := sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000))

	tests := []struct {
		name           string
		fee            sdk.Coins
		expectedAmount *sdk.Coin
		wantErr        bool
		errContains    string
	}{
		{
			name:    "valid: single utia coin with positive amount",
			fee:     sdk.NewCoins(validCoin),
			wantErr: false,
		},
		{
			name:        "invalid: empty coins",
			fee:         sdk.Coins{},
			wantErr:     true,
			errContains: "exactly one fee coin",
		},
		{
			name:        "invalid: multiple coins",
			fee:         sdk.NewCoins(validCoin, sdk.NewCoin("other", math.NewInt(500))),
			wantErr:     true,
			errContains: "exactly one fee coin",
		},
		{
			name:        "invalid: wrong denom",
			fee:         sdk.NewCoins(sdk.NewCoin("wrongdenom", math.NewInt(1000))),
			wantErr:     true,
			errContains: "requires utia denom",
		},
		{
			name:        "invalid: zero amount",
			fee:         sdk.Coins{sdk.NewCoin(appconsts.BondDenom, math.NewInt(0))},
			wantErr:     true,
			errContains: "requires positive amount",
		},
		{
			name:           "valid: matches expected amount",
			fee:            sdk.NewCoins(validCoin),
			expectedAmount: &validCoin,
			wantErr:        false,
		},
		{
			name:           "invalid: does not match expected amount",
			fee:            sdk.NewCoins(validCoin),
			expectedAmount: func() *sdk.Coin { c := sdk.NewCoin(appconsts.BondDenom, math.NewInt(2000)); return &c }(),
			wantErr:        true,
			errContains:    "does not equal expected fee",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateProtocolFee(tc.fee, tc.expectedAmount)
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
