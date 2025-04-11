package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/test/util"
	"github.com/celestiaorg/celestia-app/v4/x/minfee/keeper"
	minfeetypes "github.com/celestiaorg/celestia-app/v4/x/minfee/types"
)

func TestMigrateParams(t *testing.T) {
	tests := []struct {
		name           string
		expectedParams minfeetypes.Params
	}{
		{
			name:           "success",
			expectedParams: minfeetypes.DefaultParams(),
		},
		{
			name:           "success: non default",
			expectedParams: minfeetypes.NewParams(math.LegacyMustNewDecFromStr("0.000005")), // non default value
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testApp, _ := util.SetupTestAppWithGenesisValSet(app.DefaultInitialConsensusParams())
			ctx := testApp.GetBaseApp().NewContext(true)

			subspace, ok := testApp.MinFeeKeeper.GetParamsKeeper().GetSubspace(minfeetypes.ModuleName)
			require.True(t, ok, "failed to get subspace")
			subspace.Set(ctx, minfeetypes.KeyNetworkMinGasPrice, tt.expectedParams.NetworkMinGasPrice)

			migrator := keeper.NewMigrator(testApp.MinFeeKeeper)
			err := migrator.MigrateParams(ctx)
			require.NoError(t, err)
			require.Equal(t, tt.expectedParams, testApp.MinFeeKeeper.GetParams(ctx))
		})
	}
}
