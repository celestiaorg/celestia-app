package types_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v6/x/forwarding/types"
	"github.com/stretchr/testify/require"
)

func TestDefaultParams(t *testing.T) {
	params := types.DefaultParams()

	// Default MinForwardAmount should be zero (disabled)
	require.True(t, params.MinForwardAmount.IsZero(), "default MinForwardAmount should be zero")
}

func TestParamsValidate(t *testing.T) {
	testCases := []struct {
		name        string
		params      types.Params
		expectError bool
	}{
		{
			name:        "default params are valid",
			params:      types.DefaultParams(),
			expectError: false,
		},
		{
			name: "zero min forward amount is valid",
			params: types.Params{
				MinForwardAmount: math.ZeroInt(),
			},
			expectError: false,
		},
		{
			name: "positive min forward amount is valid",
			params: types.Params{
				MinForwardAmount: math.NewInt(1000),
			},
			expectError: false,
		},
		{
			name: "large min forward amount is valid",
			params: types.Params{
				MinForwardAmount: math.NewInt(1_000_000_000_000),
			},
			expectError: false,
		},
		{
			name: "negative min forward amount is invalid",
			params: types.Params{
				MinForwardAmount: math.NewInt(-1),
			},
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.params.Validate()

			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestParamsString(t *testing.T) {
	params := types.Params{
		MinForwardAmount: math.NewInt(1000),
	}

	str := params.String()
	require.Contains(t, str, "1000")
}
