package cmd

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// TestLocalDevnetValidatorPrivValGRPC checks that the local devnet validators
// start with the privval gRPC listener that init.sh configures. The insecure
// override has to come from the start command, not config.toml, so volumes
// initialized before the override existed keep working.
func TestLocalDevnetValidatorPrivValGRPC(t *testing.T) {
	initScript, err := os.ReadFile("../../../local_devnet/init.sh")
	require.NoError(t, err)
	match := regexp.MustCompile(`priv_validator_grpc_laddr = "([^"\\]+)\\?"`).FindSubmatch(initScript)
	require.NotNil(t, match, "init.sh must set priv_validator_grpc_laddr")
	require.NotContains(t, string(initScript), "priv_validator_grpc_allow_insecure",
		"set the override in start-validator.sh so existing volumes get it")

	startScript, err := os.ReadFile("../../../local_devnet/start-validator.sh")
	require.NoError(t, err)
	startLine := strings.ReplaceAll(string(startScript), "\\\n", " ")
	_, startArgs, ok := strings.Cut(startLine, "exec celestia-appd start ")
	require.True(t, ok, "start-validator.sh must exec celestia-appd start")
	startArgs, _, _ = strings.Cut(startArgs, "\n")

	cmd := &cobra.Command{Use: "start"}
	cmd.FParseErrWhitelist.UnknownFlags = true
	addStartFlags(cmd)
	require.NoError(t, cmd.ParseFlags(strings.Fields(startArgs)))

	sctx := server.NewDefaultContext()
	sctx.Config.PrivValidatorGRPCListenAddr = string(match[1])
	cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey, sctx))

	require.NoError(t, allowInsecurePrivValGRPC(cmd, sctx.Logger))
	require.NoError(t, sctx.Config.ValidatePrivValidatorGRPCExposure())
}
