package cmd

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v11/scripts/hardfork"
	"github.com/spf13/cobra"
)

func recoveryScriptCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "recovery-script",
		Short: "Print the hard fork recovery script of this release",
		Long: `Print the hard fork recovery script of this release to stdout.

Example:
  celestia-appd recovery-script > recovery.sh
  docker run --rm --entrypoint celestia-appd <image> recovery-script > recovery.sh
`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprint(cmd.OutOrStdout(), hardfork.RecoveryScript)
			return err
		},
	}
}
