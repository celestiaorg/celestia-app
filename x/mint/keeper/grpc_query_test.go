package keeper_test //nolint:all

import (
	gocontext "context"
	"testing"

	"github.com/stretchr/testify/suite"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/celestiaorg/celestia-app/v3/app"
	testutil "github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/celestiaorg/celestia-app/x/mint/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type MintTestSuite struct {
	suite.Suite

	app         *app.App
	ctx         sdk.Context
	queryClient types.QueryClient
}

func (suite *MintTestSuite) SetupTest() {
	testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	ctx := testApp.NewContext(false, tmproto.Header{})

	queryHelper := baseapp.NewQueryServerTestHelper(ctx, testApp.InterfaceRegistry())
	types.RegisterQueryServer(queryHelper, testApp.MintKeeper)
	queryClient := types.NewQueryClient(queryHelper)

	suite.app = testApp
	suite.ctx = ctx

	suite.queryClient = queryClient
}

func (suite *MintTestSuite) TestGRPC() {
	app, ctx, queryClient := suite.app, suite.ctx, suite.queryClient

	inflation, err := queryClient.InflationRate(gocontext.Background(), &types.QueryInflationRateRequest{})
	suite.Require().NoError(err)
	suite.Require().Equal(inflation.InflationRate, app.MintKeeper.GetMinter(ctx).InflationRate)

	annualProvisions, err := queryClient.AnnualProvisions(gocontext.Background(), &types.QueryAnnualProvisionsRequest{})
	suite.Require().NoError(err)
	suite.Require().Equal(annualProvisions.AnnualProvisions, app.MintKeeper.GetMinter(ctx).AnnualProvisions)

	genesisTime, err := queryClient.GenesisTime(gocontext.Background(), &types.QueryGenesisTimeRequest{})
	suite.Require().NoError(err)
	suite.Require().Equal(genesisTime.GenesisTime, app.MintKeeper.GetGenesisTime(ctx).GenesisTime)
}

func TestMintTestSuite(t *testing.T) {
	suite.Run(t, new(MintTestSuite))
}
