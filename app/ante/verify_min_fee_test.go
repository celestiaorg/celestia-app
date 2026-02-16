package ante

import (
	"strings"
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrimTrailingZeros(t *testing.T) {
	tests := []struct {
		name string
		dec  math.LegacyDec
		want string
	}{
		{
			name: "gas price 0.004",
			dec:  math.LegacyNewDecWithPrec(4, 3),
			want: "0.004",
		},
		{
			name: "gas price 0.002",
			dec:  math.LegacyNewDecWithPrec(2, 3),
			want: "0.002",
		},
		{
			name: "whole number 1",
			dec:  math.LegacyNewDec(1),
			want: "1",
		},
		{
			name: "whole number 800",
			dec:  math.LegacyNewDec(800),
			want: "800",
		},
		{
			name: "zero",
			dec:  math.LegacyNewDec(0),
			want: "0",
		},
		{
			name: "0.1",
			dec:  math.LegacyNewDecWithPrec(1, 1),
			want: "0.1",
		},
		{
			name: "0.123456",
			dec:  math.LegacyNewDecWithPrec(123456, 6),
			want: "0.123456",
		},
		{
			name: "1.5",
			dec:  math.LegacyNewDecWithPrec(15, 1),
			want: "1.5",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := trimTrailingZeros(tc.dec)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestVerifyMinFee(t *testing.T) {
	tests := []struct {
		name         string
		fee          math.Int
		gas          uint64
		minGasPrice  math.LegacyDec
		errMsg       string
		wantErr      bool
		wantContains []string
	}{
		{
			name:        "fee meets minimum, no error",
			fee:         math.NewInt(800),
			gas:         200_000,
			minGasPrice: math.LegacyNewDecWithPrec(4, 3), // 0.004
			wantErr:     false,
		},
		{
			name:        "fee exceeds minimum, no error",
			fee:         math.NewInt(1000),
			gas:         200_000,
			minGasPrice: math.LegacyNewDecWithPrec(4, 3), // 0.004
			errMsg:      "insufficient minimum gas price for this node",
			wantErr:     false,
		},
		{
			name:        "zero fee includes denomination in error",
			fee:         math.NewInt(0),
			gas:         200_000,
			minGasPrice: math.LegacyNewDecWithPrec(4, 3), // 0.004
			errMsg:      "insufficient minimum gas price for this node",
			wantErr:     true,
			wantContains: []string{
				"0" + appconsts.BondDenom,    // "0utia"
				"800" + appconsts.BondDenom,  // "800utia"
				"0.004",                      // min gas price without trailing zeros
				appconsts.BondDenom + "/gas", // denomination per gas unit
			},
		},
		{
			name:        "low fee includes denomination in error",
			fee:         math.NewInt(10),
			gas:         200_000,
			minGasPrice: math.LegacyNewDecWithPrec(4, 3), // 0.004
			errMsg:      "insufficient gas price for the network",
			wantErr:     true,
			wantContains: []string{
				"10" + appconsts.BondDenom,  // "10utia"
				"800" + appconsts.BondDenom, // "800utia"
				"0.004",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyMinFee(tc.fee, tc.gas, tc.minGasPrice, tc.errMsg)
			if tc.wantErr {
				require.Error(t, err)
				errStr := err.Error()
				for _, s := range tc.wantContains {
					assert.Contains(t, errStr, s)
				}
				// Verify the error does NOT contain 18 decimal places
				assert.False(t, strings.Contains(errStr, "000000000000000000"),
					"error should not contain 18 trailing zeros, got: %s", errStr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
