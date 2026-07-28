package keeper_test

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v10/app"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/x/minfee/types"
	"github.com/stretchr/testify/require"
)

// TestGenesisRoundTripPreservesParams sets a non-default network min gas price,
// exports genesis, and imports it into a fresh app. A default-value round-trip
// would pass even with ExportGenesis dropping Params, so the non-default value
// is what makes this test meaningful.
func TestGenesisRoundTripPreservesParams(t *testing.T) {
	testApp, _, _ := testutil.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
	sdkCtx := testApp.NewContext(false)

	want := types.NewParams(sdkmath.LegacyNewDecWithPrec(5, 3)) // 0.005utia, non-default
	require.NotEqual(t, types.DefaultNetworkMinGasPrice, want.NetworkMinGasPrice,
		"test requires a non-default value")
	testApp.MinFeeKeeper.SetParams(sdkCtx, want)

	exported := testApp.MinFeeKeeper.ExportGenesis(sdkCtx)
	require.Equal(t, want.NetworkMinGasPrice, exported.Params.NetworkMinGasPrice)
	require.Equal(t, want.NetworkMinGasPrice, exported.NetworkMinGasPrice,
		"deprecated top-level field must stay in sync with params")
	require.NoError(t, types.ValidateGenesis(exported))

	freshApp, _, _ := testutil.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
	freshCtx := freshApp.NewContext(false)
	require.NoError(t, freshApp.MinFeeKeeper.InitGenesis(freshCtx, *exported))
	require.Equal(t, want, freshApp.MinFeeKeeper.GetParams(freshCtx))
}
