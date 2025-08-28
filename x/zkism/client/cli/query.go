package cli

import (
	"fmt"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/spf13/cobra"
)

// NewQueryIsmCmd creates and returns the query command for a ZK execution ISM.
func NewQueryIsmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "ism [ism-id]",
		Short:   "Query a ZK execution ISM from a given ISM identifier",
		Long:    "Query a ZK execution Interchain Security Module (ISM) from a given ISM identifier.",
		Example: fmt.Sprintf("%s query %s ism 0x726f757465725f69736d00000000000000000000000000000000000000000000", version.AppName, types.ModuleName),
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			_, err = util.DecodeHexAddress(args[0])
			if err != nil {
				return fmt.Errorf("ism identifier is not a valid hex address")
			}

			req := &types.QueryIsmRequest{
				Id: args[0],
			}

			res, err := queryClient.Ism(cmd.Context(), req)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewQueryIsmsCmd creates and returns the query command for all ZK execution ISMs.
func NewQueryIsmsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "isms",
		Short:   "Query all ZK execution ISMs",
		Long:    "Query all ZK execution Interchain Security Module (ISM).",
		Example: fmt.Sprintf("%s query %s isms", version.AppName, types.ModuleName),
		Args:    cobra.NoArgs,
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

			req := &types.QueryIsmsRequest{
				Pagination: pageReq,
			}

			res, err := queryClient.Isms(cmd.Context(), req)
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}
