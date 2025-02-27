package app_test

import (
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/test/util"
)

func TestExportAppStateAndValidators(t *testing.T) {
	forZeroHeight := true
	jailAllowedAddrs := []string{}
	testApp, _ := util.SetupTestApp(t)

	// advance one block
	_, _ = testApp.FinalizeBlock(&abci.RequestFinalizeBlock{})
	_, _ = testApp.Commit()

	exported, err := testApp.ExportAppStateAndValidators(forZeroHeight, jailAllowedAddrs)
	require.NoError(t, err)
	require.NotNil(t, exported)
	require.Equal(t, uint64(4), exported.ConsensusParams.Version.App)
}
