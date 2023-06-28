package test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	testutil "github.com/celestiaorg/celestia-app/test/util"
	sdkupgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
	"github.com/stretchr/testify/require"
)

func TestRemoval(t *testing.T) {
	app, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	sftwrUpgrd := sdkupgradetypes.MsgSoftwareUpgrade{}
	router := app.MsgServiceRouter()
	handler := router.Handler(&sftwrUpgrd)
	require.Nil(t, handler)
}
