//go:build multiplexer

package cmd

import (
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/internal/embedding"
	"github.com/celestiaorg/celestia-app/v10/multiplexer/appd"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestUnsupportedFlags(t *testing.T) {
	// v6-v9 lack only the flags added in v10.
	require.Equal(t, map[string]struct{}{"otel-endpoint": {}, "fibre-promise-cache": {}, "privval-grpc-allow-insecure": {}}, unsupportedFlags(9))
	require.Equal(t, unsupportedFlags(9), unsupportedFlags(6))

	// v4 and v5 also lack flags added in v6.
	require.Contains(t, unsupportedFlags(5), "bypass-config-overrides")
	require.Contains(t, unsupportedFlags(4), "delayed-precommit-timeout")
	require.NotContains(t, unsupportedFlags(5), "with-comet")

	// v3 (cosmos-sdk v0.46) also lacks cosmos-sdk v0.50 flags.
	for _, appVersion := range []uint64{1, 2, 3} {
		flags := unsupportedFlags(appVersion)
		require.Contains(t, flags, "otel-endpoint")
		require.Contains(t, flags, "bypass-config-overrides")
		require.Contains(t, flags, "with-comet")
		require.Contains(t, flags, "log_no_color")
		require.Contains(t, flags, "shutdown-grace")
	}
}

// Check both directions: new native flags must be classified, and flags added
// to an embedded release must no longer be filtered. Run by make test-multiplexer.
func TestUnsupportedFlagsMatchEmbeddedBinaries(t *testing.T) {
	root := NewRootCmd()
	// Execute help through the same SDK entry point as main, which registers
	// logging and other global flags before Cobra parses the start command.
	root.SetArgs([]string{"start", "--help"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	require.NoError(t, svrcmd.Execute(root, app.EnvPrefix, t.TempDir()))
	start, _, err := root.Find([]string{"start"})
	require.NoError(t, err)
	flagPattern := regexp.MustCompile(`(?m)^\s+(?:-\w, )?--([\w.-]+)\b`)
	for i, embedded := range []func() (string, []byte, error){
		embedding.CelestiaAppV3, embedding.CelestiaAppV4, embedding.CelestiaAppV5,
		embedding.CelestiaAppV6, embedding.CelestiaAppV7, embedding.CelestiaAppV8, embedding.CelestiaAppV9,
	} {
		version, data, err := embedded()
		require.NoError(t, err)
		t.Run(version, func(t *testing.T) {
			binary, err := appd.New(version, data)
			require.NoError(t, err)
			help := binary.CreateExecCommand("start", "--help")
			help.Stdout, help.Stderr = nil, nil
			output, err := help.CombinedOutput()
			require.NoError(t, err, "%s", output)
			supported := map[string]bool{}
			for _, match := range flagPattern.FindAllStringSubmatch(string(output), -1) {
				supported[match[1]] = true
			}
			unsupported := unsupportedFlags(uint64(i + 3))
			start.Flags().VisitAll(func(flag *pflag.Flag) {
				if !supported[flag.Name] {
					// Hidden and deprecated flags are absent from help. --help prevents
					// node startup while still asking pflag to validate the flag name.
					probe := binary.CreateExecCommand("start", "--help", "--"+flag.Name+"="+flag.DefValue)
					probe.Stdout, probe.Stderr = nil, nil
					output, err := probe.CombinedOutput()
					if strings.Contains(string(output), "unknown flag: --"+flag.Name) {
						supported[flag.Name] = false
					} else {
						require.NoError(t, err, "probe --%s: %s", flag.Name, output)
						supported[flag.Name] = true
					}
				}
				_, excluded := unsupported[flag.Name]
				require.Equal(t, !supported[flag.Name], excluded, "update unsupportedFlags for %s flag --%s", version, flag.Name)
			})
			for name := range unsupported {
				require.NotNil(t, start.Flags().Lookup(name), "remove obsolete unsupported flag --%s", name)
			}
		})
	}
}
