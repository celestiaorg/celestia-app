package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/celestiaorg/celestia-app/x/mint/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
)

// GetQueryCmd returns the CLI query commands for the mint module.
func GetQueryCmd() *cobra.Command {
	mintQueryCmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Querying commands for the mint module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	mintQueryCmd.AddCommand(
		GetCmdQueryParams(),
		GetCmdQueryInflationRate(),
		GetCmdQueryAnnualProvisions(),
	)

	return mintQueryCmd
}

// GetCmdQueryParams implements a command to return the current mint
// parameters.
func GetCmdQueryParams() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "params",
		Short: "Query the current mint parameters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			params := &types.QueryParamsRequest{}
			res, err := queryClient.Params(cmd.Context(), params)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(&res.Params)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

// GetCmdQueryInflationRate implements a command to return the current mint
// inflation rate.
func GetCmdQueryInflationRate() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inflation",
		Short: "Query the current inflation rate",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			request := &types.QueryInflationRateRequest{}
			res, err := queryClient.InflationRate(cmd.Context(), request)
			if err != nil {
				return err
			}

			return clientCtx.PrintString(fmt.Sprintf("%s\n", res.InflationRate))
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

// GetCmdQueryAnnualProvisions implements a command to return the current mint
// annual provisions.
func GetCmdQueryAnnualProvisions() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "annual-provisions",
		Short: "Query the current annual provisions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			request := &types.QueryAnnualProvisionsRequest{}
			res, err := queryClient.AnnualProvisions(cmd.Context(), request)
			if err != nil {
				return err
			}

			return clientCtx.PrintString(fmt.Sprintf("%s\n", res.AnnualProvisions))
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

// GetCmdQueryGenesisTime implements a command to return the genesis time.
func GetCmdQueryGenesisTime() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "genesis-time",
		Short: "Query the genesis time",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)

			params := &types.QueryAnnualProvisionsRequest{}
			res, err := queryClient.AnnualProvisions(cmd.Context(), params)
			if err != nil {
				return err
			}

			return clientCtx.PrintString(fmt.Sprintf("%s\n", res.AnnualProvisions))
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}
