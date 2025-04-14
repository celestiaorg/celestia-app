package cmd

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/01builders/nova/abci"
	"github.com/01builders/nova/appd"
)

func TestNewPassthroughCmd(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		versions       abci.Versions
		expectedErrStr string
		expectedOutput string
	}{
		{
			name:           "required arguments not specified",
			args:           []string{},
			versions:       []abci.Version{},
			expectedErrStr: "requires at least 1 arg(s), only received 0",
		},
		{
			name: "version not found existing versions",
			args: []string{"2"},
			versions: abci.Versions{
				newVersion(1, &appd.Appd{}),
			},
			expectedErrStr: "version 2 requires the latest app, use the command directly without passthrough",
		},
		{
			name: "underlying appd is nil",
			args: []string{"1", "q"},
			versions: abci.Versions{
				newVersion(1, nil),
			},
			expectedErrStr: "no binary available for version 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewPassthroughCmd(tt.versions)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			output, err := executeCommand(t, cmd, tt.args...)

			if tt.expectedErrStr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectedErrStr)
			} else {
				require.NoError(t, err)
				require.Contains(t, output, tt.expectedOutput)
			}
		})
	}
}

// newVersion creates a new abci.Version with given appversion and appd.Appd instance.
func newVersion(appVersion uint64, app *appd.Appd) abci.Version {
	return abci.Version{
		AppVersion: appVersion,
		Appd:       app,
	}
}

// executeCommand executes the cobra command with the specified args.
func executeCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	cmd.SetArgs(args)
	output, err := cmd.ExecuteC()
	if err != nil {
		return "", err
	}
	return output.Name(), nil
}
