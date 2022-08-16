package cli

import (
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/spf13/cobra"
)

// TODO change gravity to qgb
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
		CmdGetValsetConfirm(),
		CmdGetDataCommitmentConfirm(),
		// CmdGovAirdropProposal(), // TODO: investigate if we want these
		// CmdGovUnhaltBridgeProposal(),
	}...)

	return gravityTxCmd
}
