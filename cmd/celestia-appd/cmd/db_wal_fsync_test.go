package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/libs/walfsync"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestStartHelpListsAcceptUnsyncedDBCrash(t *testing.T) {
	rootCmd := NewRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"start", "--help"})
	require.NoError(t, rootCmd.Execute())
	require.Contains(t, out.String(), "--"+walfsync.AcceptFlag)
}

// newDBWALFsyncCmd returns a start command with a server context rooted at a
// temporary home, and the path of the node's crash marker.
func newDBWALFsyncCmd(t *testing.T, dbPath string) (*cobra.Command, *server.Context, string) {
	cmd := &cobra.Command{Use: "start"}
	addDBWALFsyncFlag(cmd)
	sctx := server.NewDefaultContext()
	sctx.Viper = viper.New()
	sctx.Config = cmtcfg.DefaultConfig()
	sctx.Config.SetRoot(t.TempDir())
	sctx.Config.DBBackend = "goleveldb"
	if dbPath != "" {
		sctx.Config.DBPath = dbPath
	}
	cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey, sctx))
	marker := filepath.Join(sctx.Config.DBDir(), walfsync.MarkerFile)
	require.NoError(t, os.MkdirAll(filepath.Dir(marker), 0o700))
	require.NoError(t, os.WriteFile(marker, []byte("another-boot\n"), 0o600))
	return cmd, sctx, marker
}

func TestSetupDBWALFsync(t *testing.T) {
	t.Run("accept flag reaches AcceptUnsyncedDBCrash", func(t *testing.T) {
		cmd, sctx, marker := newDBWALFsyncCmd(t, "")
		require.NoError(t, cmd.Flags().Set(walfsync.AcceptFlag, "true"))
		require.NoError(t, setupDBWALFsync(cmd, sctx.Logger))
		require.NoFileExists(t, marker)
	})
	t.Run("without the flag the node decides", func(t *testing.T) {
		cmd, sctx, marker := newDBWALFsyncCmd(t, "")
		require.NoError(t, setupDBWALFsync(cmd, sctx.Logger))
		require.FileExists(t, marker)
	})
	t.Run("app data outside db_dir is checked here", func(t *testing.T) {
		cmd, sctx, marker := newDBWALFsyncCmd(t, "cmtdata")
		require.ErrorIs(t, setupDBWALFsync(cmd, sctx.Logger), walfsync.ErrUnsyncedCrash)
		require.FileExists(t, marker)

		require.NoError(t, cmd.Flags().Set(walfsync.AcceptFlag, "true"))
		require.NoError(t, setupDBWALFsync(cmd, sctx.Logger))
		require.NoFileExists(t, marker)
	})
}

func TestClearDBWALFsyncMarkers(t *testing.T) {
	_, sctx, marker := newDBWALFsyncCmd(t, "")
	appMarker := filepath.Join(sctx.Config.RootDir, "data", embeddedAppMarkerFile)
	require.NoError(t, os.WriteFile(appMarker, []byte("another-boot\n"), 0o600))

	require.NoError(t, clearDBWALFsyncMarkers(sctx.Config))
	require.NoFileExists(t, marker)
	require.NoFileExists(t, appMarker)
	require.NoError(t, clearDBWALFsyncMarkers(sctx.Config), "no markers")
}

func TestRollbackClearsDBWALFsyncMarkers(t *testing.T) {
	rollbackCmd, _, err := NewRootCmd().Find([]string{"rollback"})
	require.NoError(t, err)
	require.NotNil(t, rollbackCmd.PostRunE)
}

func TestDBWALDirs(t *testing.T) {
	cfg := cmtcfg.DefaultConfig()
	cfg.SetRoot("/home")
	appData := filepath.Join(cfg.RootDir, "data")
	require.Equal(t, []string{cfg.DBDir()}, dbWALDirs(cfg, appData))
	cfg.DBPath = "cmtdata"
	cfg.BlockstorePath = "blocks"
	require.Equal(t, []string{"/home/cmtdata", "/home/blocks", "/home/data"}, dbWALDirs(cfg, appData))
	require.True(t, isWithin("/home/data/application.db", "/home/data"))
	require.False(t, isWithin("/home/database", "/home/data"))
}
