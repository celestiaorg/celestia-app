package cmd

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/01builders/nova/abci"
)

// NewPassthroughCmd creates a command that allows executing commands on any app version.
// This enables direct interaction with older app versions for debugging or older queries.
func NewPassthroughCmd(versions abci.Versions) *cobra.Command {
	cmd := &cobra.Command{
		Use:                "passthrough [version] [command]",
		Args:               cobra.MinimumNArgs(1),
		DisableFlagParsing: true,
		Short:              "Execute a command on a specific app version",
		Long: `Execute a command on a specific app version.
This allows interacting with older app versions for debugging or older queries.`,
		Example: `passthrough v3 status`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) >= 2 && strings.EqualFold("start", args[1]) {
				return errors.New("cannot passthrough start command")
			}

			versionStr := strings.TrimPrefix(args[0], "v")
			version, err := strconv.ParseUint(versionStr, 10, 64)
			if err != nil {
				return fmt.Errorf("failed to parse version: %w", err)
			}

			if versions.ShouldUseLatestApp(version) {
				return fmt.Errorf("version %d requires the latest app, use the command directly without passthrough", version)
			}

			appVersion, err := versions.GetForAppVersion(version)
			if err != nil {
				return fmt.Errorf("no version found for %d: %w", version, err)
			}

			// ensure we have a valid appd instance
			if appVersion.Appd == nil {
				return fmt.Errorf("no binary available for version %d", version)
			}

			// prepare the command to be executed
			execCmd := appVersion.Appd.CreateExecCommand(args[1:]...)
			return execCmd.Run()
		},
	}

	return cmd
}
