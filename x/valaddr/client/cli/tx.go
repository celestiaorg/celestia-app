package cli

import (
	"github.com/celestiaorg/celestia-app/v8/x/valaddr/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"
)

// GetTxCmd returns the transaction commands for the valaddr module
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Valaddr transaction subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		CmdSetFibreProviderInfo(),
	)

	return cmd
}

// CmdSetFibreProviderInfo broadcasts a MsgSetFibreProviderInfo transaction
func CmdSetFibreProviderInfo() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-host [host]",
		Short: "Set the fibre provider host for your validator",
		Long: `Set the fibre provider host for your validator.
The transaction must be signed by the validator's account.

Example:
$ celestia-appd tx valaddr set-host <host> --from <validator-account-key>
`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			// Convert account address to validator operator address
			addr := clientCtx.GetFromAddress().Bytes()
			valAddr := sdk.ValAddress(addr)

			msg := &types.MsgSetFibreProviderInfo{
				Signer: valAddr.String(),
				Host:   args[0],
			}

			if err := msg.ValidateBasic(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
