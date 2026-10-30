package cmd

import (
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v11/app"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	"github.com/stretchr/testify/require"
)

func TestNewAppServerAppliesTimeoutFlags(t *testing.T) {
	opts := simtestutil.AppOptionsMap{
		flags.FlagHome:              t.TempDir(),
		flags.FlagChainID:           "test",
		server.FlagPruning:          "nothing",
		TimeoutCommitFlag:           5 * time.Millisecond,
		DelayedPrecommitTimeoutFlag: 7 * time.Millisecond,
		DynamicTimeoutCommitFlag:    3 * time.Millisecond,
	}
	celestiaApp, ok := NewAppServer(log.NewNopLogger(), dbm.NewMemDB(), nil, opts).(*app.App)
	require.True(t, ok)

	got := celestiaApp.TimeoutInfo(11)
	require.Equal(t, 5*time.Millisecond, got.TimeoutCommit)
	require.Equal(t, 7*time.Millisecond, got.DelayedPrecommitTimeout)
	require.Equal(t, 3*time.Millisecond, got.DynamicTimeoutCommit)
}
