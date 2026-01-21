package types_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v7/x/forwarding/types"
	"github.com/stretchr/testify/require"
)

func TestDefaultGenesis(t *testing.T) {
	genesis := types.DefaultGenesis()

	// Verify default genesis has default params
	require.NotNil(t, genesis)
	require.NoError(t, genesis.Params.Validate())
}

func TestValidateGenesis(t *testing.T) {
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
			name: "genesis with empty params is valid",
			genesis: &types.GenesisState{
				Params: types.Params{},
			},
			expectError: false,
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
		Params: types.Params{},
	}

	str := genesis.String()
	require.NotEmpty(t, str)
}
