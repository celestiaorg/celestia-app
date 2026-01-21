package keeper_test

import (
	"context"
	"testing"

	"github.com/celestiaorg/celestia-app/v7/x/forwarding/keeper"
	"github.com/celestiaorg/celestia-app/v7/x/forwarding/types"
	"github.com/stretchr/testify/require"
)

// NOTE: Full keeper tests require warp infrastructure setup.
// These tests cover the stateless genesis operations.
// Full integration tests are in test/interop/forwarding_integration_test.go

func TestGenesisRoundTrip(t *testing.T) {
	// Create a minimal keeper (stateless - doesn't need dependencies for genesis)
	k := keeper.Keeper{}

	// Init with default genesis (no-op for stateless module)
	defaultGenesis := types.DefaultGenesis()
	err := k.InitGenesis(context.Background(), defaultGenesis)
	require.NoError(t, err)

	// Export and verify it returns empty state
	exported, err := k.ExportGenesis(context.Background())
	require.NoError(t, err)
	require.NotNil(t, exported)

	// Validate the exported genesis
	require.NoError(t, exported.Validate())
}

func TestGenesisInit_EmptyState(t *testing.T) {
	k := keeper.Keeper{}

	// Init with empty genesis state
	emptyGenesis := &types.GenesisState{}
	err := k.InitGenesis(context.Background(), emptyGenesis)
	require.NoError(t, err)
}
