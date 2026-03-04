package cli

import (
	"context"
	"fmt"

	"github.com/celestiaorg/celestia-app/v8/x/valaddr/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/spf13/cobra"
)

// GetQueryCmd returns the cli query commands for the valaddr module
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Querying commands for the valaddr module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		CmdQueryFibreProviderInfo(),
		CmdQueryAllFibreProviders(),
	)

	return cmd
}

// CmdQueryFibreProviderInfo queries the fibre provider info for a specific validator
func CmdQueryFibreProviderInfo() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provider [consensus-address]",
		Short: "Query fibre provider info for a specific validator by consensus address",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.FibreProviderInfo(
				context.Background(),
				&types.QueryFibreProviderInfoRequest{
					ValidatorConsensusAddress: args[0],
				},
			)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

// CmdQueryAllFibreProviders queries all fibre providers
func CmdQueryAllFibreProviders() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "providers",
		Short: "Query all fibre providers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.AllFibreProviders(
				context.Background(),
				&types.QueryAllFibreProvidersRequest{},
			)
			if err != nil {
				return err
			}

			if len(res.Providers) == 0 {
				fmt.Println("No fibre providers found")
				return nil
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
