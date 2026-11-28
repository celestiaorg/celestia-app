package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/celestiaorg/celestia-app/v3/internal/walfsync"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	cmtcfg "github.com/tendermint/tendermint/config"
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

func TestSetupWALFsyncAcceptFlag(t *testing.T) {
	for _, accept := range []bool{false, true} {
		cmd := &cobra.Command{Use: "start"}
		cmd.Flags().Bool(flagWithTendermint, true, "")
		addWALFsyncFlag(cmd)
		sctx := server.NewDefaultContext()
		sctx.Viper = viper.New()
		sctx.Config = cmtcfg.DefaultConfig()
		sctx.Config.SetRoot(t.TempDir())
		cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey, sctx))
		marker := filepath.Join(sctx.Config.RootDir, "data", walfsync.MarkerFile)
		require.NoError(t, os.MkdirAll(filepath.Dir(marker), 0o700))
		require.NoError(t, os.WriteFile(marker, []byte("another-boot\n"), 0o600))

		if accept {
			require.NoError(t, cmd.Flags().Set(walfsync.AcceptFlag, "true"))
			require.NoError(t, setupWALFsync(cmd, sctx.Logger))
			require.NoFileExists(t, marker)
		} else {
			require.ErrorIs(t, setupWALFsync(cmd, sctx.Logger), walfsync.ErrUnsyncedCrash)
			require.FileExists(t, marker)
		}
	}
}
