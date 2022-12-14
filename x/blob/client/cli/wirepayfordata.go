package cli

import (
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
)

func CmdWirePayForBlob() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "payForBlob [hexNamespace] [hexBlob]",
		Short: "Pay for a data blob to be published to the Celestia blockchain",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			// get the account name
			accName := clientCtx.GetFromName()
			if accName == "" {
				return errors.New("no account name provided, please use the --from flag")
			}

			// decode the namespace
			namespace, err := hex.DecodeString(args[0])
			if err != nil {
				return fmt.Errorf("failure to decode hex namespace: %w", err)
			}

			// decode the blob
			blob, err := hex.DecodeString(args[1])
			if err != nil {
				return fmt.Errorf("failure to decode hex blob: %w", err)
			}

			// TODO: allow the user to override the share version via a new flag
			// See https://github.com/celestiaorg/celestia-app/issues/1041
			pfbMsg, err := types.NewMsgPayForBlob(clientCtx.FromAddress.String(), namespace, blob)
			if err != nil {
				return err
			}

			// run message checks
			if err = pfbMsg.ValidateBasic(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), pfbMsg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
