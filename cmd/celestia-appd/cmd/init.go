package cmd

import (
	"github.com/cosmos/cosmos-sdk/x/genutil/client/cli"
	"github.com/spf13/cobra"

	"github.com/celestiaorg/celestia-app/v4/app"
)

// InitCmd returns a custom version of the default init command with improved help docs.
func InitCmd() *cobra.Command {
	cmd := cli.InitCmd(app.ModuleBasics, app.DefaultNodeHome)

	cmd.Short = "Initialize configuration files for a Celestia consensus node"
	cmd.Long = "This command sets up a genesis file and default configuration for either a validator or a full node."
	cmd.Example = "celestia-appd init node-name --chain-id celestia"

	return cmd
}
