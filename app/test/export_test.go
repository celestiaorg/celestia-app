package app_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/test/util"
	"github.com/stretchr/testify/require"
)

func TestExportAppStateAndValidators(t *testing.T) {
	testApp, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), "genesisAcc")
	exported, err := testApp.ExportAppStateAndValidators(true, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, exported)
	require.Equal(t, appconsts.LatestVersion, exported.ConsensusParams.Version.App)
}
