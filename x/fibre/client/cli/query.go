package cli

import (
	"context"
	"fmt"

	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cobra"
)

// GetQueryCmd returns the CLI query commands for this module
func GetQueryCmd() *cobra.Command {
	// Group fibre queries under a subcommand
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("Querying commands for the %s module", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		CmdQueryParams(),
		CmdQueryEscrowAccount(),
		CmdQueryWithdrawals(),
		CmdQueryIsPaymentProcessed(),
	)

	return cmd
}

// CmdQueryParams implements the params query command.
func CmdQueryParams() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "params",
		Args:  cobra.NoArgs,
		Short: "Query the current fibre parameters",
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.Params(context.Background(), &types.QueryParamsRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

// CmdQueryEscrowAccount implements the escrow-account query command.
func CmdQueryEscrowAccount() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "escrow-account [signer]",
		Args:  cobra.ExactArgs(1),
		Short: "Query an escrow account by signer address",
		Long: `Query an escrow account by signer address.

Example:
$ celestia-appd query fibre escrow-account celestia1...
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.EscrowAccount(context.Background(), &types.QueryEscrowAccountRequest{
				Signer: args[0],
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

// CmdQueryWithdrawals implements the withdrawals query command.
func CmdQueryWithdrawals() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "withdrawals [signer]",
		Args:  cobra.ExactArgs(1),
		Short: "Query all withdrawals for an escrow account by signer address",
		Long: `Query all withdrawals for an escrow account by signer address.

Example:
$ celestia-appd query fibre withdrawals celestia1...
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.Withdrawals(context.Background(), &types.QueryWithdrawalsRequest{
				Signer: args[0],
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

// CmdQueryIsPaymentProcessed implements the is-payment-processed query command.
func CmdQueryIsPaymentProcessed() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "is-payment-processed [payment-promise-hash]",
		Args:  cobra.ExactArgs(1),
		Short: "Query whether a payment promise has been processed",
		Long: `Query whether a payment promise has been processed by its hash.

Example:
$ celestia-appd query fibre is-payment-processed 0x1234...
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.IsPaymentProcessed(context.Background(), &types.QueryIsPaymentProcessedRequest{
				PromiseHash: []byte(args[0]),
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
