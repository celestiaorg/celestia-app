package cmd

import (
	"bytes"
	"fmt"
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
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	"github.com/rs/zerolog"
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

// TestDisableOptimisticExecution loads the config the way the start command
// does and checks that the app.toml key or the env var turns OE off.
func TestDisableOptimisticExecution(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)
	// The start command prefixes env vars with the binary name: CELESTIA_APPD_ for celestia-appd.
	envVar := strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(filepath.Base(exe) + "_" + disableOptimisticExecutionKey))

	for _, tc := range []struct {
		name, appToml, env string
		enabled            bool
	}{
		{name: "default", enabled: true},
		{name: "app.toml", appToml: disableOptimisticExecutionKey + " = true\n", enabled: false},
		{name: "env", env: "true", enabled: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(envVar, tc.env)
			home := t.TempDir()
			if tc.appToml != "" {
				require.NoError(t, os.MkdirAll(filepath.Join(home, "config"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(home, "config", "app.toml"), []byte(tc.appToml), 0o600))
			}
			opts := startContext(t, home).Viper
			opts.Set(flags.FlagChainID, "test")

			var logs bytes.Buffer
			logger := log.NewLogger(zerolog.SyncWriter(&logs), log.ColorOption(false), log.LevelOption(zerolog.InfoLevel))
			NewAppServer(logger, dbm.NewMemDB(), nil, opts)
			require.Contains(t, logs.String(), fmt.Sprintf("optimistic execution enabled=%v", tc.enabled))
		})
	}
}
