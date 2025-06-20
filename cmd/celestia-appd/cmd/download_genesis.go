package cmd

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

func downloadGenesisCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "download-genesis [chain-id]",
		Short: "Download genesis file from https://github.com/celestiaorg/networks",
		Long: "Download genesis file from https://github.com/celestiaorg/networks.\n" +
			fmt.Sprintf("The first argument should be a known chain-id. Ex. %s\n", app.ChainIDs()) +
			"If no argument is provided, defaults to celestia.\n",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			chainID := app.GetChainIDOrDefault(args)
			outputFile := server.GetServerContextFromCmd(cmd).Config.GenesisFile()
			return app.DownloadGenesis(chainID, outputFile)
		},
	}

	return cmd
}
