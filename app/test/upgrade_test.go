package app_test

import (
	"testing"

	v1 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v2"
	"github.com/celestiaorg/celestia-app/v2/test/util"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmversion "github.com/tendermint/tendermint/proto/tendermint/version"
)

func TestUpgradeAppVersion(t *testing.T) {
	testApp, _ := util.SetupTestApp(t, 3)

	supportedVersions := []uint64{v1.Version, v2.Version}

	require.Equal(t, supportedVersions, testApp.SupportedVersions())

	testApp.BeginBlock(abci.RequestBeginBlock{Header: tmproto.Header{
		Height:  2,
		Version: tmversion.Consensus{App: 1},
	}})
	// app version should not have changed yet
	require.EqualValues(t, 1, testApp.AppVersion())
	respEndBlock := testApp.EndBlock(abci.RequestEndBlock{Height: 2})
	// now the app version changes
	require.NotNil(t, respEndBlock.ConsensusParamUpdates.Version)
	require.EqualValues(t, 2, respEndBlock.ConsensusParamUpdates.Version.AppVersion)
	require.EqualValues(t, 2, testApp.AppVersion())
}
