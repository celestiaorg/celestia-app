package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
// does and checks that the app.toml key (hand-written or generated) or the env var turns OE off.
func TestDisableOptimisticExecution(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)
	// The start command prefixes env vars with the binary name: CELESTIA_APPD_ for celestia-appd.
	envVar := strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(filepath.Base(exe) + "_" + disableOptimisticExecutionKey))

	for _, tc := range []struct {
		name, appToml, env string
		generated          bool
		enabled            bool
	}{
		{name: "default", enabled: true},
		{name: "app.toml", appToml: disableOptimisticExecutionKey + " = true\n", enabled: false},
		{name: "generated app.toml", generated: true, enabled: false},
		{name: "env", env: "true", enabled: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(envVar, tc.env)
			home := t.TempDir()
			if tc.appToml != "" {
				require.NoError(t, os.MkdirAll(filepath.Join(home, "config"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(home, "config", "app.toml"), []byte(tc.appToml), 0o600))
			}
			if tc.generated {
				flipGeneratedOEKey(t, home)
			}
			opts := startContext(t, home).Viper
			opts.Set(flags.FlagChainID, "test")

			var logs bytes.Buffer
			logger := log.NewLogger(zerolog.SyncWriter(&logs), log.ColorOption(false), log.LevelOption(zerolog.InfoLevel))
			celestiaApp, ok := NewAppServer(logger, dbm.NewMemDB(), nil, opts).(*app.App)
			require.True(t, ok)
			require.Contains(t, logs.String(), fmt.Sprintf("optimistic execution enabled=%v", tc.enabled))
			// BaseApp has no accessor for OE; its unexported optimisticExec is nil iff OE is off.
			require.Equal(t, tc.enabled, !reflect.ValueOf(celestiaApp.BaseApp).Elem().FieldByName("optimisticExec").IsNil())
		})
	}
}

// flipGeneratedOEKey lets the first start write app.toml, checks the OE key is
// top level (before any [section]) and sets it to true.
func flipGeneratedOEKey(t *testing.T, home string) {
	startContext(t, home)
	path := filepath.Join(home, "config", "app.toml")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	topLevel, _, _ := strings.Cut(string(data), "\n[")
	line := "\n" + disableOptimisticExecutionKey + " = false\n"
	require.Contains(t, topLevel, line)
	flipped := strings.Replace(string(data), line, "\n"+disableOptimisticExecutionKey+" = true\n", 1)
	require.NoError(t, os.WriteFile(path, []byte(flipped), 0o600))
}
