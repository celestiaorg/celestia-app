package upgrade

import (
	"github.com/spf13/cobra"

	cli "github.com/cosmos/cosmos-sdk/x/upgrade/client/cli"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
)

// GetTxCmd returns the transaction commands for this module
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   upgradetypes.ModuleName,
		Short: "Upgrade transaction subcommands",
	}

	return cmd
}

// GetQueryCmd returns the parent command for all x/upgrade CLi query commands.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   upgradetypes.ModuleName,
		Short: "Querying commands for the upgrade module",
	}

	cmd.AddCommand(
		cli.GetModuleVersionsCmd(),
	)

	return cmd
}
