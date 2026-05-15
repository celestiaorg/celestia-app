package types_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v9/x/consensustimeouts/types"
	"github.com/stretchr/testify/require"
)

// TestDefaultGenesis_Valid asserts the default genesis state validates.
func TestDefaultGenesis_Valid(t *testing.T) {
	require.NoError(t, types.DefaultGenesis().Validate())
}

// TestGenesis_Validate_DelegatesToParams asserts that an invalid Params in a
// GenesisState surfaces from g.Validate() via the params validator.
func TestGenesis_Validate_DelegatesToParams(t *testing.T) {
	// Start from defaults, then break TimeoutCommit so it is out of range.
	params := types.DefaultParams()
	params.TimeoutCommit = 0

	g := types.NewGenesisState(params)
	err := g.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "timeout_commit")
}
