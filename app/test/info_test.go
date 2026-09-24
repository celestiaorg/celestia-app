package app_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/app"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestInfoPreservesUncommittedGenesisAppVersion(t *testing.T) {
	params := app.DefaultConsensusParams()
	testApp, _, _ := testutil.NewTestAppWithGenesisSet(params)
	ctx := testApp.NewContext(false)

	version, err := testApp.AppVersion(ctx)
	require.NoError(t, err)
	require.Equal(t, params.Version.App, version)
	require.NotEqual(t, baseapp.InitialAppVersion, version)

	for range 2 {
		info, err := testApp.Info(&abci.RequestInfo{})
		require.NoError(t, err)
		require.Zero(t, info.LastBlockHeight)
		require.Equal(t, baseapp.InitialAppVersion, info.AppVersion)

		got, err := testApp.AppVersion(ctx)
		require.NoError(t, err)
		require.Equal(t, version, got, "Info must not reset the initialized version")
	}
}

func TestInfoUsesCommittedAppVersion(t *testing.T) {
	params := app.DefaultConsensusParams()
	testApp, _ := testutil.SetupTestAppWithGenesisValSet(params)
	version := params.Version.App
	commitID := testApp.LastCommitID()

	execBlock(t, testApp, func(ctx sdk.Context) {
		require.NoError(t, testApp.SetAppVersion(ctx, version+1))

		for range 2 {
			info, err := testApp.Info(&abci.RequestInfo{})
			require.NoError(t, err)
			require.Equal(t, version, info.AppVersion)
			require.Equal(t, commitID.Version, info.LastBlockHeight)
			require.Equal(t, commitID.Hash, info.LastBlockAppHash)

			got, err := testApp.AppVersion(ctx)
			require.NoError(t, err)
			require.Equal(t, version+1, got, "Info must not overwrite the pending version")
		}
	})

	info, err := testApp.Info(&abci.RequestInfo{})
	require.NoError(t, err)
	require.Equal(t, version+1, info.AppVersion)
	require.Equal(t, testApp.LastBlockHeight(), info.LastBlockHeight)
	require.Equal(t, testApp.LastCommitID().Hash, info.LastBlockAppHash)
}
