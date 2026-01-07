package cli

import (
	"fmt"
	"strconv"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/x/forwarding/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/spf13/cobra"
)

const flagIsmID = "ism-id"

// GetTxCmd returns the transaction commands for this module.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s transactions subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(CmdCreateInterchainAccountsRouter())
	return cmd
}

func CmdCreateInterchainAccountsRouter() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-ica-router [origin-mailbox] [receiver-domain] [receiver-contract] [gas]",
		Short: "Create an interchain accounts router and enroll the first remote router",
		Args:  cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			originMailbox, err := util.DecodeHexAddress(args[0])
			if err != nil {
				return err
			}

			receiverDomain, err := strconv.ParseUint(args[1], 10, 32)
			if err != nil {
				return err
			}

			gas, ok := math.NewIntFromString(args[3])
			if !ok {
				return fmt.Errorf("invalid gas amount: %s", args[3])
			}

			var ismID *util.HexAddress
			ismIDStr, err := cmd.Flags().GetString(flagIsmID)
			if err != nil {
				return err
			}
			if ismIDStr != "" {
				decoded, err := util.DecodeHexAddress(ismIDStr)
				if err != nil {
					return err
				}
				ismID = &decoded
			}

			msg := &types.MsgCreateInterchainAccountsRouter{
				Owner:         clientCtx.GetFromAddress().String(),
				OriginMailbox: originMailbox,
				IsmId:         ismID,
				RemoteRouter: &types.RemoteRouter{
					ReceiverDomain:   uint32(receiverDomain),
					ReceiverContract: args[2],
					Gas:              gas,
				},
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String(flagIsmID, "", "Optional ISM identifier (hex address)")
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
