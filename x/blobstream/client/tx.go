package client

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/x/blobstream/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/spf13/cobra"
)

// GetTxCmd returns the transaction commands for this module
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s transactions subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(CmdRegisterEVMAddress())

	return cmd
}

func CmdRegisterEVMAddress() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register [valAddress] [evmAddress]",
		Short: "Register an EVM address for a validator",
		Long: `Registers an EVM address for a validator. This address will be used to
sign attestations as part of the Blobstream protocol. Only the validator, as the signer,
can register an EVM address. To change the EVM address, the validator can simply
send a new message overriding the previous one.
`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			valAddress, evmAddress := args[0], args[1]
			msg := &types.MsgRegisterEVMAddress{
				ValidatorAddress: valAddress,
				EvmAddress:       evmAddress,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
