package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/celestiaorg/celestia-app/v8/x/forwarding/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"
)

// GetTxCmd returns the transaction commands for the forwarding module.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Forwarding module transaction subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(CmdForward())

	return cmd
}

// CmdForward returns a CLI command for submitting a MsgForward transaction.
func CmdForward() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "forward [forward-addr] [dest-domain] [dest-recipient] --max-igp-fee [fee]",
		Short: "Forward tokens at a forwarding address to their committed destination",
		Long: `Forward all tokens at a derived forwarding address to the committed destination via Hyperlane warp transfer.

The relayer (signer) pays both Celestia gas and Hyperlane IGP fees. Use 'query forwarding quote-fee'
to estimate the required IGP fee before submitting.

Example:
  celestia-appd tx forwarding forward celestia1abc... 42161 0x000000000000000000000000742d35cc6634c0532925a3b844bc9e7595f00000 \
    --max-igp-fee 1000utia --from relayer`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			forwardAddr := args[0]
			destDomainStr := args[1]
			destRecipient := args[2]

			// Sanitize: ensure 0x prefix for consistency
			if !strings.HasPrefix(strings.ToLower(destRecipient), "0x") {
				destRecipient = "0x" + destRecipient
			}

			destDomain, err := strconv.ParseUint(destDomainStr, 10, 32)
			if err != nil {
				return fmt.Errorf("invalid dest_domain: %w", err)
			}

			maxIgpFeeStr, err := cmd.Flags().GetString("max-igp-fee")
			if err != nil {
				return err
			}

			maxIgpFee, err := sdk.ParseCoinNormalized(maxIgpFeeStr)
			if err != nil {
				return fmt.Errorf("invalid max-igp-fee: %w", err)
			}

			msg := types.NewMsgForward(
				clientCtx.GetFromAddress().String(),
				forwardAddr,
				uint32(destDomain),
				destRecipient,
				maxIgpFee,
			)

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String("max-igp-fee", "1000000utia", "Maximum IGP fee to pay per token (default: 1000000utia)")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
