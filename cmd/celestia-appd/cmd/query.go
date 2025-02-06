package cmd

import (
	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/rpc"
	authcmd "github.com/cosmos/cosmos-sdk/x/auth/client/cli"
	"github.com/spf13/cobra"
)

func queryCommand() *cobra.Command {
	command := &cobra.Command{
		Use:                        "query",
		Aliases:                    []string{"q"},
		Short:                      "Querying subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	command.AddCommand(
		authcmd.GetAccountCmd(),
		rpc.ValidatorCommand(),
		rpc.BlockCommand(),
		authcmd.QueryTxsByEventsCmd(),
		authcmd.QueryTxCmd(),
	)

	app.ModuleBasics.AddQueryCommands(command)
	command.PersistentFlags().String(flags.FlagChainID, "", "The network chain ID")

	return command
}
