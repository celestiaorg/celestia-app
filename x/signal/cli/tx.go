package cli

import (
	"fmt"
	"strconv"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"

	"github.com/celestiaorg/celestia-app/v4/x/signal/types"
)

// GetTxCmd returns the transaction commands for this module.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s transactions subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(CmdSignalVersion())
	cmd.AddCommand(CmdTryUpgrade())
	return cmd
}

func CmdSignalVersion() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "signal version",
		Short: "Signal a software upgrade for the specified version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			version, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return err
			}

			addr := clientCtx.GetFromAddress().Bytes()
			valAddr := sdk.ValAddress(addr)
			msg := types.NewMsgSignalVersion(valAddr.String(), version)
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func CmdTryUpgrade() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "try-upgrade",
		Short: "Try to perform a software upgrade",
		Long: `This command will submit a TryUpgrade message to tally all
the signal votes. If a quorum has been reached, the network will upgrade
to the signalled version at the following height.
`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			msg := types.NewMsgTryUpgrade(clientCtx.GetFromAddress())
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
