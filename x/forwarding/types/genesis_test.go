package types_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v8/x/forwarding/types"
	"github.com/stretchr/testify/require"
)

func TestDefaultGenesis(t *testing.T) {
	genesis := types.DefaultGenesis()

	// Verify default genesis is not nil and valid
	require.NotNil(t, genesis)
	require.NoError(t, genesis.Validate())
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
			name:        "empty genesis is valid",
			genesis:     &types.GenesisState{},
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
