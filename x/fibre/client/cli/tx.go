package cli

import (
	"fmt"
	"strings"

	"github.com/celestiaorg/celestia-app/v6/x/fibre/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"
)

// GetTxCmd returns the transaction commands for the fibre module
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s transactions subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		CmdSetFibreProviderInfo(),
		CmdRemoveFibreProviderInfo(),
	)

	return cmd
}

// CmdSetFibreProviderInfo implements the set-provider-info command
func CmdSetFibreProviderInfo() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-provider-info [ip-address]",
		Short: "Set fibre provider info for your validator",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			ipAddress := strings.TrimSpace(args[0])
			if ipAddress == "" {
				return fmt.Errorf("IP address cannot be empty")
			}

			// Get validator address from the key
			fromAddr := clientCtx.GetFromAddress()
			validatorAddr := sdk.ValAddress(fromAddr)

			msg := &types.MsgSetFibreProviderInfo{
				ValidatorAddress: validatorAddr.String(),
				IpAddress:        ipAddress,
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

// CmdRemoveFibreProviderInfo implements the remove-provider-info command
func CmdRemoveFibreProviderInfo() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove-provider-info [validator-address]",
		Short: "Remove fibre provider info for an inactive validator",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			validatorAddr := strings.TrimSpace(args[0])
			if validatorAddr == "" {
				return fmt.Errorf("validator address cannot be empty")
			}

			removerAddr := clientCtx.GetFromAddress()

			msg := &types.MsgRemoveFibreProviderInfo{
				ValidatorAddress: validatorAddr,
				RemoverAddress:   removerAddr.String(),
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