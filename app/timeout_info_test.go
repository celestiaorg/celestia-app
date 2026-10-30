package app_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/test/util"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/stretchr/testify/require"
)

var (
	v10TimeoutInfo = abci.TimeoutInfo{
		TimeoutPropose:          3000 * time.Millisecond,
		TimeoutProposeDelta:     500 * time.Millisecond,
		TimeoutPrevote:          2000 * time.Millisecond,
		TimeoutPrevoteDelta:     500 * time.Millisecond,
		TimeoutPrecommit:        3000 * time.Millisecond,
		TimeoutPrecommitDelta:   500 * time.Millisecond,
		TimeoutCommit:           500 * time.Millisecond,
		DelayedPrecommitTimeout: 2100 * time.Millisecond,
	}
	v11TimeoutInfo = abci.TimeoutInfo{
		TimeoutPropose:        1500 * time.Millisecond,
		TimeoutProposeDelta:   500 * time.Millisecond,
		TimeoutPrevote:        1000 * time.Millisecond,
		TimeoutPrevoteDelta:   500 * time.Millisecond,
		TimeoutPrecommit:      1500 * time.Millisecond,
		TimeoutPrecommitDelta: 500 * time.Millisecond,
		TimeoutCommit:         time.Nanosecond,
		DynamicTimeoutCommit:  800 * time.Millisecond,
	}
)

// TestTimeoutInfo checks that InitChain, FinalizeBlock and Info return the
// timeouts of the app version in state.
func TestTimeoutInfo(t *testing.T) {
	for _, tc := range []struct {
		appVersion uint64
		want       abci.TimeoutInfo
	}{
		{appVersion: 10, want: v10TimeoutInfo},
		{appVersion: 11, want: v11TimeoutInfo},
	} {
		t.Run(fmt.Sprintf("v%d", tc.appVersion), func(t *testing.T) {
			testApp := app.New(log.NewNopLogger(), tmdb.NewMemDB(), nil, 0, 0, util.EmptyAppOptions{}, baseapp.SetChainID(util.ChainID))
			initRes, finalizeRes, infoRes := runFirstBlock(t, testApp, tc.appVersion)
			require.Equal(t, tc.want, initRes.TimeoutInfo, "InitChain")
			require.Equal(t, tc.want, finalizeRes.TimeoutInfo, "FinalizeBlock")
			require.Equal(t, tc.appVersion, infoRes.AppVersion)
			require.Equal(t, tc.want, infoRes.TimeoutInfo, "Info")
		})
	}

	t.Run("overrides apply to every version that has the timeout", func(t *testing.T) {
		testApp := app.New(log.NewNopLogger(), tmdb.NewMemDB(), nil, 7*time.Millisecond, 5*time.Millisecond, util.EmptyAppOptions{})
		testApp.SetDynamicTimeoutCommit(3 * time.Millisecond)
		for _, appVersion := range []uint64{10, 11} {
			want := appconsts.GetConsensusTimeouts(appVersion)
			got := testApp.TimeoutInfo(appVersion)
			require.Equal(t, 5*time.Millisecond, got.TimeoutCommit)
			require.Equal(t, 7*time.Millisecond, got.DelayedPrecommitTimeout)
			require.Equal(t, want.TimeoutPropose, got.TimeoutPropose)
		}
		require.Equal(t, 3*time.Millisecond, testApp.TimeoutInfo(11).DynamicTimeoutCommit)
		require.Zero(t, testApp.TimeoutInfo(10).DynamicTimeoutCommit, "v10 keeps the legacy pacing")
	})

	// Production runs the v10 -> v11 upgrade block in the embedded v10 app, so
	// this guards later upgrades that the native app executes.
	t.Run("upgrade block returns the new version's timeouts", func(t *testing.T) {
		testApp, _ := newEndBlockerTestApp(t)
		ctx := testApp.NewUncachedContext(false, cmtproto.Header{})
		require.NoError(t, testApp.SetAppVersion(ctx, 10))
		signalAndTryUpgrade(t, testApp, ctx, 11)
		upgrade, err := testApp.SignalKeeper.GetUpgrade(ctx, nil)
		require.NoError(t, err)
		ctx = ctx.WithBlockHeight(upgrade.Upgrade.UpgradeHeight)

		res, err := testApp.EndBlocker(ctx)
		require.NoError(t, err)
		require.Equal(t, v11TimeoutInfo, res.TimeoutInfo)
	})
}

// runFirstBlock initialises testApp at appVersion, finalizes and commits the
// first block, and returns the InitChain, FinalizeBlock and Info responses.
func runFirstBlock(t *testing.T, testApp *app.App, appVersion uint64) (*abci.ResponseInitChain, *abci.ResponseFinalizeBlock, *abci.ResponseInfo) {
	t.Helper()
	genesisState, valSet, _ := util.GenesisStateWithSingleValidator(testApp)
	stateBytes, err := json.Marshal(genesisState)
	require.NoError(t, err)
	cparams := app.DefaultConsensusParams()
	cparams.Version.App = appVersion

	initRes, err := testApp.InitChain(&abci.RequestInitChain{
		Time:            util.GenesisTime,
		ConsensusParams: cparams,
		AppStateBytes:   stateBytes,
		ChainId:         util.ChainID,
	})
	require.NoError(t, err)
	finalizeRes, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Time:               util.GenesisTime,
		Height:             1,
		NextValidatorsHash: valSet.Hash(),
	})
	require.NoError(t, err)
	_, err = testApp.Commit()
	require.NoError(t, err)
	infoRes, err := testApp.Info(&abci.RequestInfo{})
	require.NoError(t, err)
	return initRes, finalizeRes, infoRes
}
