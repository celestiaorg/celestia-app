package app_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v4/app"
	abci "github.com/cometbft/cometbft/abci/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	tmversion "github.com/cometbft/cometbft/proto/tendermint/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExportAppStateAndValidators(t *testing.T) {
	t.Run("should return exported app for version 1", func(t *testing.T) {
		forZeroHeight := true
		jailAllowedAddrs := []string{}
		testApp, _ := SetupTestAppWithUpgradeHeight(t, 3)

		exported, err := testApp.ExportAppStateAndValidators(forZeroHeight, jailAllowedAddrs)
		require.NoError(t, err)
		assert.NotNil(t, exported)
		assert.Equal(t, uint64(1), exported.ConsensusParams.Version.App)
	})
	t.Run("should return exported app for version 2", func(t *testing.T) {
		forZeroHeight := false
		jailAllowedAddrs := []string{}

		testApp, _ := SetupTestAppWithUpgradeHeight(t, 3)
		upgradeToV2(t, testApp)

		exported, err := testApp.ExportAppStateAndValidators(forZeroHeight, jailAllowedAddrs)
		require.NoError(t, err)
		assert.NotNil(t, exported)
		// TODO: the following assertion is commented out because the exported app does not populate consensus params.version
		// assert.Equal(t, uint64(2), exported.ConsensusParams.Version.App)
	})
}

func upgradeToV2(t *testing.T, testApp *app.App) {
	testApp.BeginBlock(abci.RequestBeginBlock{Header: tmproto.Header{
		Height:  2,
		Version: tmversion.Consensus{App: 1},
	}})
	// Upgrade from v1 -> v2
	testApp.EndBlock(abci.RequestEndBlock{Height: 2})
	testApp.Commit()
	require.EqualValues(t, 2, testApp.AppVersion())
	testApp.BeginBlock(abci.RequestBeginBlock{Header: tmproto.Header{
		Height:  3,
		Version: tmversion.Consensus{App: 2},
	}})
	testApp.EndBlock(abci.RequestEndBlock{Height: 3})
	testApp.Commit()
	require.EqualValues(t, 3, testApp.LastBlockHeight())
}
