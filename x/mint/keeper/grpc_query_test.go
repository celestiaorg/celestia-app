package keeper_test //nolint:all

import (
	gocontext "context"
	"testing"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/app"
	testutil "github.com/celestiaorg/celestia-app/v4/test/util"
	"github.com/celestiaorg/celestia-app/v4/x/mint/types"
)

func TestGRPC(t *testing.T) {
	testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	ctx := testApp.NewContext(true)

	queryHelper := baseapp.NewQueryServerTestHelper(ctx, testApp.GetEncodingConfig().InterfaceRegistry)
	types.RegisterQueryServer(queryHelper, testApp.MintKeeper)
	queryClient := types.NewQueryClient(queryHelper)

	inflation, err := queryClient.InflationRate(gocontext.Background(), &types.QueryInflationRateRequest{})
	require.NoError(t, err)
	require.Equal(t, inflation.InflationRate, testApp.MintKeeper.GetMinter(ctx).InflationRate)

	annualProvisions, err := queryClient.AnnualProvisions(gocontext.Background(), &types.QueryAnnualProvisionsRequest{})
	require.NoError(t, err)
	require.Equal(t, annualProvisions.AnnualProvisions, testApp.MintKeeper.GetMinter(ctx).AnnualProvisions)

	genesisTime, err := queryClient.GenesisTime(gocontext.Background(), &types.QueryGenesisTimeRequest{})
	require.NoError(t, err)
	require.Equal(t, genesisTime.GenesisTime, testApp.MintKeeper.GetGenesisTime(ctx).GenesisTime)
}
