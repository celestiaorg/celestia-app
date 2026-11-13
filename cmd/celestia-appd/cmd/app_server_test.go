package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v11/app"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
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

// TestDisableOptimisticExecutionFlag runs a node built by NewAppServer and
// checks that the flag stops ProcessProposal from starting speculative runs.
func TestDisableOptimisticExecutionFlag(t *testing.T) {
	t.Parallel()
	start := &cobra.Command{}
	addStartFlags(start)
	require.NotNil(t, start.Flags().Lookup(FlagDisableOptimisticExecution))

	for _, disable := range []bool{false, true} {
		t.Run(fmt.Sprintf("disable=%v", disable), func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			logger := log.NewLogger(zerolog.SyncWriter(&logs), log.ColorOption(false))
			cfg := newRollbackHome(t)
			cfg.AppOptions.Set(FlagDisableOptimisticExecution, disable)
			creator := func(_ log.Logger, db dbm.DB, w io.Writer, opts servertypes.AppOptions) servertypes.Application {
				return NewAppServer(logger, db, w, opts)
			}
			runNode(t, cfg, creator, 4)

			require.Contains(t, logs.String(), fmt.Sprintf("optimistic execution enabled=%v", !disable))
			require.Equal(t, !disable, strings.Contains(logs.String(), "OE started"), "speculative runs")
		})
	}
}

// TestAppConfigDisablesOptimisticExecution checks that a generated app.toml has
// the setting at top level and that setting it to true turns OE off.
func TestAppConfigDisablesOptimisticExecution(t *testing.T) {
	home := t.TempDir()
	require.Equal(t, false, startContext(t, home).Viper.Get(FlagDisableOptimisticExecution))

	path := filepath.Join(home, "config", "app.toml")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	setting := FlagDisableOptimisticExecution + " = "
	at := strings.Index(string(data), "\n"+setting+"false\n")
	require.Less(t, at, strings.Index(string(data), "\n["), "must precede the first [section]")
	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(string(data), setting+"false", setting+"true", 1)), 0o600))
	require.Equal(t, true, startContext(t, home).Viper.Get(FlagDisableOptimisticExecution))
}
