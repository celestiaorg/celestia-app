package cli

import (
	"context"
	"fmt"

	"github.com/celestiaorg/celestia-app/v6/x/fibre/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cobra"
)

// GetQueryCmd returns the cli query commands for the fibre module
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("Querying commands for the %s module", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		CmdQueryFibreProviderInfo(),
		CmdQueryAllActiveFibreProviders(),
	)

	return cmd
}

// CmdQueryFibreProviderInfo implements the fibre provider info query command
func CmdQueryFibreProviderInfo() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provider [validator-address]",
		Short: "Query fibre provider info for a specific validator",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			req := &types.QueryFibreProviderInfoRequest{
				ValidatorAddress: args[0],
			}

			res, err := queryClient.FibreProviderInfo(context.Background(), req)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

// CmdQueryAllActiveFibreProviders implements the all active fibre providers query command
func CmdQueryAllActiveFibreProviders() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "active-providers",
		Short: "Query all active fibre providers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}

			req := &types.QueryAllActiveFibreProvidersRequest{
				Pagination: pageReq,
			}

			res, err := queryClient.AllActiveFibreProviders(context.Background(), req)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	flags.AddPaginationFlagsToCmd(cmd, "all active fibre providers")

	return cmd
}