package minfee_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v2/app"
	v2 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v2"
	testutil "github.com/celestiaorg/celestia-app/v2/test/util"
	"github.com/celestiaorg/celestia-app/v2/x/minfee"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestQueryNetworkMinGasPrice(t *testing.T) {
	testApp, _, _ := testutil.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
	queryServer := minfee.NewQueryServerImpl(testApp.ParamsKeeper)

	sdkCtx := testApp.NewContext(false, tmproto.Header{Height: 1})
	ctx := sdk.WrapSDKContext(sdkCtx)

	// Perform a query for the network minimum gas price
	resp, err := queryServer.NetworkMinGasPrice(ctx, &minfee.QueryNetworkMinGasPrice{})
	require.NoError(t, err)

	// Check the response
	require.Equal(t, v2.NetworkMinGasPrice, resp.NetworkMinGasPrice.MustFloat64())
}
