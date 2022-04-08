package cli

import (
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/spf13/cobra"
)

func GetTxCmd() *cobra.Command {
	// needed for governance proposal txs in cli case
	// internal check prevents double registration in node case
	// keeper.RegisterProposalTypes()

	//nolint: exhaustivestruct
	gravityTxCmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "qgb transaction subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	gravityTxCmd.AddCommand([]*cobra.Command{
		CmdSetOrchestratorAddress(),
		// CmdGovAirdropProposal(), // TODO: investigate if we want these
		// CmdGovUnhaltBridgeProposal(),
	}...)

	return gravityTxCmd
}

func CmdSetOrchestratorAddress() *cobra.Command {
	//nolint: exhaustivestruct
	cmd := &cobra.Command{
		Use:   "set-orchestrator-address [validator-address] [orchestrator-address] [ethereum-address]",
		Short: "Allows validators to delegate their voting responsibilities to a given key.",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			cliCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			msg := types.MsgSetOrchestratorAddress{
				Validator:    args[0],
				Orchestrator: args[1],
				EthAddress:   args[2],
			}
			if err := msg.ValidateBasic(); err != nil {
				return err
			}
			// Send it
			return tx.GenerateOrBroadcastTxCLI(cliCtx, cmd.Flags(), &msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
