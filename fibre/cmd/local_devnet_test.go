package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

// TestLocalDevnetFibreCommands checks that the Fibre service commands in the
// local devnet compose file produce a config that passes validation. The
// services reach the validator signer by hostname, so they need an explicit
// plaintext override.
func TestLocalDevnetFibreCommands(t *testing.T) {
	raw, err := os.ReadFile("../../local_devnet/compose.yaml")
	require.NoError(t, err)
	var compose struct {
		Services map[string]struct {
			Command []string `yaml:"command"`
		} `yaml:"services"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &compose))

	for _, name := range []string{"fibre-1", "fibre-2"} {
		t.Run(name, func(t *testing.T) {
			service, ok := compose.Services[name]
			require.True(t, ok, "service %s not found", name)
			require.GreaterOrEqual(t, len(service.Command), 2)
			require.Equal(t, []string{"fibre", "start"}, service.Command[:2])

			// Drop --home so the command uses a temp dir instead of /data.
			var args []string
			for i := 2; i < len(service.Command); i++ {
				if service.Command[i] == "--"+flagHome {
					i++
					continue
				}
				args = append(args, service.Command[i])
			}

			cmd, got := newTestStartCmd(t, t.TempDir())
			cmd.SetArgs(args)
			require.NoError(t, cmd.Execute())
			require.True(t, got.SignerGRPCAllowInsecure)
			require.NoError(t, got.Validate())
		})
	}
}
