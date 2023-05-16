package test

import (
	"testing"

	testutil "github.com/celestiaorg/celestia-app/test/util"
	sdkupgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
	"github.com/stretchr/testify/require"
)

func TestRemoval(t *testing.T) {
	app, _ := testutil.SetupTestAppWithGenesisValSet()
	sftwrUpgrd := sdkupgradetypes.MsgSoftwareUpgrade{}
	router := app.MsgServiceRouter()
	handler := router.Handler(&sftwrUpgrd)
	require.Nil(t, handler)
}
