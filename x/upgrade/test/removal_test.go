package test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	testutil "github.com/celestiaorg/celestia-app/test/util"
	sdkupgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
	"github.com/stretchr/testify/require"
)

func TestRemoval(t *testing.T) {
	genesisTime := time.Date(2023, 1, 1, 1, 1, 1, 1, time.UTC).UTC()
	app, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), genesisTime)
	sftwrUpgrd := sdkupgradetypes.MsgSoftwareUpgrade{}
	router := app.MsgServiceRouter()
	handler := router.Handler(&sftwrUpgrd)
	require.Nil(t, handler)
}
