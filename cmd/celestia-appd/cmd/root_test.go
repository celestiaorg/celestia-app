package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// TestReplaceLoggerStripsColorCodes verifies that logs written to a file do not
// contain ANSI color escape codes. See https://github.com/celestiaorg/celestia-app/issues/4966
func TestReplaceLoggerStripsColorCodes(t *testing.T) {
	logFilePath := filepath.Join(t.TempDir(), "test.log")

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String(FlagLogToFile, "", "")
	require.NoError(t, cmd.Flags().Set(FlagLogToFile, logFilePath))

	// Use a default server context which enables colored output by default.
	sctx := server.NewDefaultContext()
	sctx.Viper = viper.New()
	ctx := context.WithValue(context.Background(), server.ServerContextKey, sctx)
	cmd.SetContext(ctx)

	require.NoError(t, replaceLogger(cmd))

	sctx = server.GetServerContextFromCmd(cmd)
	sctx.Logger.Info("received complete proposal block", "height", 6562871, "module", "consensus")

	contents, err := os.ReadFile(logFilePath)
	require.NoError(t, err)
	require.NotEmpty(t, contents)

	// The ANSI escape character (0x1b) should not appear in the log file.
	require.NotContains(t, string(contents), "\x1b", "log file should not contain ANSI color escape codes")
}

func TestAddStartFlagsRegistersPrivValGRPCAllowInsecure(t *testing.T) {
	cmd := &cobra.Command{Use: "start"}
	addStartFlags(cmd)

	flag := cmd.Flags().Lookup(FlagPrivValGRPCAllowInsecure)
	require.NotNil(t, flag)
	require.Equal(t, "false", flag.DefValue)
}

func TestAllowInsecurePrivValGRPC(t *testing.T) {
	newCmd := func(t *testing.T) (*cobra.Command, *server.Context) {
		cmd := &cobra.Command{Use: "start"}
		cmd.Flags().Bool(FlagPrivValGRPCAllowInsecure, false, "")

		sctx := server.NewDefaultContext()
		ctx := context.WithValue(context.Background(), server.ServerContextKey, sctx)
		cmd.SetContext(ctx)
		return cmd, sctx
	}

	t.Run("flag not passed leaves the key unset", func(t *testing.T) {
		cmd, sctx := newCmd(t)
		require.NoError(t, allowInsecurePrivValGRPC(cmd, sctx.Logger))
		require.False(t, sctx.Viper.IsSet(privValGRPCAllowInsecureKey))
	})

	t.Run("flag passed sets the key to true", func(t *testing.T) {
		cmd, sctx := newCmd(t)
		require.NoError(t, cmd.Flags().Set(FlagPrivValGRPCAllowInsecure, "true"))
		require.NoError(t, allowInsecurePrivValGRPC(cmd, sctx.Logger))
		require.True(t, sctx.Viper.GetBool(privValGRPCAllowInsecureKey))
	})
}
