package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/celestiaorg/celestia-app/app"
	testutil "github.com/celestiaorg/celestia-app/test/util"
	keep "github.com/celestiaorg/celestia-app/x/mint/keeper"
	"github.com/celestiaorg/celestia-app/x/mint/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type MintKeeperTestSuite struct {
	suite.Suite

	app              *app.App
	ctx              sdk.Context
	legacyQuerierCdc *codec.AminoCodec
}

func (suite *MintKeeperTestSuite) SetupTest() {
	testApp, _ := testutil.SetupTestAppWithGenesisValSet()
	ctx := testApp.NewContext(true, tmproto.Header{})

	testApp.MintKeeper.SetParams(ctx, types.DefaultParams())
	testApp.MintKeeper.SetMinter(ctx, types.DefaultMinter())

	legacyQuerierCdc := codec.NewAminoCodec(testApp.LegacyAmino())

	suite.app = testApp
	suite.ctx = ctx
	suite.legacyQuerierCdc = legacyQuerierCdc
}

func (suite *MintKeeperTestSuite) TestNewQuerier(t *testing.T) {
	app, ctx, legacyQuerierCdc := suite.app, suite.ctx, suite.legacyQuerierCdc
	querier := keep.NewQuerier(app.MintKeeper, legacyQuerierCdc.LegacyAmino)

	query := abci.RequestQuery{
		Path: "",
		Data: []byte{},
	}

	_, err := querier(ctx, []string{types.QueryParameters}, query)
	require.NoError(t, err)

	_, err = querier(ctx, []string{types.QueryInflation}, query)
	require.NoError(t, err)

	_, err = querier(ctx, []string{types.QueryAnnualProvisions}, query)
	require.NoError(t, err)

	_, err = querier(ctx, []string{"foo"}, query)
	require.Error(t, err)
}

func (suite *MintKeeperTestSuite) TestQueryParams(t *testing.T) {
	app, ctx, legacyQuerierCdc := suite.app, suite.ctx, suite.legacyQuerierCdc
	querier := keep.NewQuerier(app.MintKeeper, legacyQuerierCdc.LegacyAmino)

	var params types.Params

	res, sdkErr := querier(ctx, []string{types.QueryParameters}, abci.RequestQuery{})
	require.NoError(t, sdkErr)

	err := app.LegacyAmino().UnmarshalJSON(res, &params)
	require.NoError(t, err)

	require.Equal(t, app.MintKeeper.GetParams(ctx), params)
}

func (suite *MintKeeperTestSuite) TestQueryInflation(t *testing.T) {
	app, ctx, legacyQuerierCdc := suite.app, suite.ctx, suite.legacyQuerierCdc
	querier := keep.NewQuerier(app.MintKeeper, legacyQuerierCdc.LegacyAmino)

	var inflation sdk.Dec

	res, sdkErr := querier(ctx, []string{types.QueryInflation}, abci.RequestQuery{})
	require.NoError(t, sdkErr)

	err := app.LegacyAmino().UnmarshalJSON(res, &inflation)
	require.NoError(t, err)

	require.Equal(t, app.MintKeeper.GetMinter(ctx).Inflation, inflation)
}

func (suite *MintKeeperTestSuite) TestQueryAnnualProvisions(t *testing.T) {
	app, ctx, legacyQuerierCdc := suite.app, suite.ctx, suite.legacyQuerierCdc
	querier := keep.NewQuerier(app.MintKeeper, legacyQuerierCdc.LegacyAmino)

	var annualProvisions sdk.Dec

	res, sdkErr := querier(ctx, []string{types.QueryAnnualProvisions}, abci.RequestQuery{})
	require.NoError(t, sdkErr)

	err := app.LegacyAmino().UnmarshalJSON(res, &annualProvisions)
	require.NoError(t, err)

	require.Equal(t, app.MintKeeper.GetMinter(ctx).AnnualProvisions, annualProvisions)
}
