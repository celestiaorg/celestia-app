package cmd

import (
	"github.com/cosmos/cosmos-sdk/x/genutil/client/cli"
	"github.com/spf13/cobra"

	"github.com/celestiaorg/celestia-app/v4/app"
)

// InitCmd returns a custom version of the default init command with improved help docs.
func InitCmd(capp *app.App) *cobra.Command {
	cmd := cli.InitCmd(capp.BasicManager, app.DefaultNodeHome)

	cmd.Short = "Initialize configuration files for a Celestia consensus node"
	cmd.Long = "This command creates a genesis file and the default configuration files for a consensus node."
	cmd.Example = "celestia-appd init node-name --chain-id celestia"

	return cmd
}
