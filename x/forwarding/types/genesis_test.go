package types_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v7/x/forwarding/types"
	"github.com/stretchr/testify/require"
)

func TestDefaultGenesis(t *testing.T) {
	genesis := types.DefaultGenesis()

	// Verify default genesis has default params
	require.NotNil(t, genesis)
	require.True(t, genesis.Params.MinForwardAmount.IsZero())
}

func TestValidateGenesis(t *testing.T) {
	validTiaCollateralTokenId := "0x000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"

	testCases := []struct {
		name        string
		genesis     *types.GenesisState
		expectError bool
	}{
		{
			name:        "nil genesis is invalid",
			genesis:     nil,
			expectError: true,
		},
		{
			name:        "default genesis is valid",
			genesis:     types.DefaultGenesis(),
			expectError: false,
		},
		{
			name: "genesis with zero min forward amount is valid",
			genesis: &types.GenesisState{
				Params: types.Params{
					MinForwardAmount: math.ZeroInt(),
				},
			},
			expectError: false,
		},
		{
			name: "genesis with positive min forward amount is valid",
			genesis: &types.GenesisState{
				Params: types.Params{
					MinForwardAmount: math.NewInt(5000),
				},
			},
			expectError: false,
		},
		{
			name: "genesis with negative min forward amount is invalid",
			genesis: &types.GenesisState{
				Params: types.Params{
					MinForwardAmount: math.NewInt(-100),
				},
			},
			expectError: true,
		},
		{
			name: "genesis with valid TiaCollateralTokenId is valid",
			genesis: &types.GenesisState{
				Params: types.Params{
					MinForwardAmount:     math.ZeroInt(),
					TiaCollateralTokenId: validTiaCollateralTokenId,
				},
			},
			expectError: false,
		},
		{
			name: "genesis with empty TiaCollateralTokenId is valid (disabled)",
			genesis: &types.GenesisState{
				Params: types.Params{
					MinForwardAmount:     math.ZeroInt(),
					TiaCollateralTokenId: "",
				},
			},
			expectError: false,
		},
		{
			name: "genesis with invalid TiaCollateralTokenId (not hex) is invalid",
			genesis: &types.GenesisState{
				Params: types.Params{
					MinForwardAmount:     math.ZeroInt(),
					TiaCollateralTokenId: "not-a-hex-address",
				},
			},
			expectError: true,
		},
		{
			name: "genesis with invalid TiaCollateralTokenId (too short) is invalid",
			genesis: &types.GenesisState{
				Params: types.Params{
					MinForwardAmount:     math.ZeroInt(),
					TiaCollateralTokenId: "0xdeadbeef",
				},
			},
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := types.ValidateGenesis(tc.genesis)

			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGenesisStateString(t *testing.T) {
	genesis := &types.GenesisState{
		Params: types.Params{
			MinForwardAmount: math.NewInt(1000),
		},
	}

	str := genesis.String()
	require.Contains(t, str, "1000")
}
