package app_test

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/celestiaorg/celestia-app/v12/app"
	"github.com/celestiaorg/celestia-app/v12/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v12/test/util"
	signaltypes "github.com/celestiaorg/celestia-app/v12/x/signal/types"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// TestExportWarnsUnexportedUpgrades checks that an export succeeds with upgrade
// state it drops and warns about it, except for an IBC upgrade plan.
func TestExportWarnsUnexportedUpgrades(t *testing.T) {
	const version = appconsts.Version + 1
	scheduleUpgrade := func(ibc bool) func(*testing.T, *app.App, sdk.Context) {
		return func(t *testing.T, a *app.App, ctx sdk.Context) {
			plan := upgradetypes.Plan{Name: "v100", Height: ctx.BlockHeight() + 2}
			require.NoError(t, a.UpgradeKeeper.ScheduleUpgrade(ctx, plan))
			if ibc {
				require.NoError(t, a.UpgradeKeeper.SetUpgradedClient(ctx, plan.Height, []byte("client")))
			}
		}
	}
	signal := func(try bool) func(*testing.T, *app.App, sdk.Context) {
		return func(t *testing.T, a *app.App, ctx sdk.Context) {
			validators, err := a.StakingKeeper.GetAllValidators(ctx)
			require.NoError(t, err)
			valAddr, err := sdk.ValAddressFromBech32(validators[0].GetOperator())
			require.NoError(t, err)
			_, err = a.SignalKeeper.SignalVersion(ctx, signaltypes.NewMsgSignalVersion(valAddr.String(), version))
			require.NoError(t, err)
			if try {
				_, err = a.SignalKeeper.TryUpgrade(ctx, signaltypes.NewMsgTryUpgrade(sdk.AccAddress(valAddr)))
				require.NoError(t, err)
			}
		}
	}
	tally := fmt.Sprintf("x/signal signals are not exported (app version %d: 1 validators); validators must signal again on the new chain\n", version)

	testCases := []struct {
		name  string
		setup func(*testing.T, *app.App, sdk.Context)
		want  string
	}{
		{
			name:  "upgrade plan after the next height",
			setup: scheduleUpgrade(false),
			want:  "x/upgrade plan \"v100\" at height 4 is not exported; schedule it again on the new chain\n",
		},
		{
			name:  "IBC upgrade plan after the next height",
			setup: scheduleUpgrade(true),
		},
		{
			name:  "pending signal upgrade",
			setup: signal(true),
			want: fmt.Sprintf("x/signal pending upgrade to app version %d at height %d is not exported; "+
				"validators must signal and run TryUpgrade again on the new chain, and the upgrade delay starts over\n",
				version, appconsts.GetUpgradeHeightDelay("", appconsts.Version)) + tally,
		},
		{
			name:  "signals only",
			setup: signal(false),
			want:  tally,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var warnings bytes.Buffer
			t.Cleanup(app.SetExportWarnings(&warnings))
			testApp, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
			height := testApp.LastBlockHeight() + 1
			_, err := testApp.FinalizeBlock(util.WithBlockHash(&abci.RequestFinalizeBlock{Height: height, Time: util.GenesisTime.Add(time.Duration(height) * time.Second)}))
			require.NoError(t, err)
			tc.setup(t, testApp, testApp.NewUncachedContext(false, cmtproto.Header{Height: height}))
			_, err = testApp.Commit()
			require.NoError(t, err)

			_, err = testApp.ExportAppStateAndValidators(false, nil, nil)
			require.NoError(t, err)
			require.Equal(t, tc.want, warnings.String())
		})
	}
}
