package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/celestiaorg/celestia-app/v7/x/forwarding/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cobra"
)

// GetQueryCmd returns the query commands for the forwarding module.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Forwarding module query subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		CmdDeriveAddress(),
		CmdQuoteFee(),
	)

	return cmd
}

// CmdDeriveAddress returns a CLI command for querying a derived forwarding address.
func CmdDeriveAddress() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "derive-address [dest-domain] [dest-recipient]",
		Short: "Derive the forwarding address for given destination parameters",
		Long: `Derive the deterministic forwarding address for a given destination domain and recipient.

Example:
  celestia-appd query forwarding derive-address 42161 0x000000000000000000000000742d35cc6634c0532925a3b844bc9e7595f00000`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			destDomainStr := args[0]
			destRecipient := args[1]

			// Sanitize: ensure 0x prefix for consistency
			if !strings.HasPrefix(strings.ToLower(destRecipient), "0x") {
				destRecipient = "0x" + destRecipient
			}

			destDomain, err := strconv.ParseUint(destDomainStr, 10, 32)
			if err != nil {
				return fmt.Errorf("invalid dest_domain: %w", err)
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.DeriveForwardingAddress(cmd.Context(), &types.QueryDeriveForwardingAddressRequest{
				DestDomain:    uint32(destDomain),
				DestRecipient: destRecipient,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

// CmdQuoteFee returns a CLI command for querying the IGP fee for forwarding.
func CmdQuoteFee() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "quote-fee [dest-domain]",
		Short: "Query the estimated IGP fee for forwarding to a destination domain",
		Long: `Query the estimated Hyperlane IGP fee required for forwarding TIA to a destination domain.
Relayers should use this to determine the max_igp_fee to provide in MsgForward.

Example:
  celestia-appd query forwarding quote-fee 42161`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			destDomainStr := args[0]
			destDomain, err := strconv.ParseUint(destDomainStr, 10, 32)
			if err != nil {
				return fmt.Errorf("invalid dest_domain: %w", err)
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.QuoteForwardingFee(cmd.Context(), &types.QueryQuoteForwardingFeeRequest{
				DestDomain: uint32(destDomain),
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
