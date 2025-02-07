package minfee_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v4/test/util"
	"github.com/celestiaorg/celestia-app/v4/x/minfee"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestQueryNetworkMinGasPrice(t *testing.T) {
	testApp, _, _ := testutil.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
	queryServer := minfee.NewQueryServerImpl(testApp.ParamsKeeper)

	sdkCtx := testApp.NewContext(false).WithBlockHeight(1)
	ctx := sdk.WrapSDKContext(sdkCtx)

	// Perform a query for the network minimum gas price
	resp, err := queryServer.NetworkMinGasPrice(ctx, &minfee.QueryNetworkMinGasPrice{})
	require.NoError(t, err)

	// Check the response
	require.Equal(t, appconsts.DefaultNetworkMinGasPrice, resp.NetworkMinGasPrice.MustFloat64())
}
