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
