package cli

import (
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/lazyledger/lazyledger-app/x/lazyledgerapp/types"
	"github.com/spf13/cobra"
)

// CmdCreatePayForMessage returns a cobra command that uses the key ring backend
// and locally running node to create and broadcast a new WirePayForMessage
// transaction.
func CmdCreatePayForMessage() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "payForMessage [hexNamespace] [hexMessage]",
		Short: "Creates a new WirePayForMessage",
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

			// get info on the key
			keyInfo, err := clientCtx.Keyring.Key(accName)
			if err != nil {
				return err
			}

			// decode the namespace
			namespace, err := hex.DecodeString(args[0])
			if err != nil {
				return fmt.Errorf("failure to decode hex namespace: %w", err)
			}

			// decode the message
			message, err := hex.DecodeString(args[1])
			if err != nil {
				return fmt.Errorf("failure to decode hex message: %w", err)
			}

			// create the PayForMessage
			pfmMsg, err := types.NewMsgWirePayForMessage(
				namespace,
				message,
				keyInfo.GetPubKey().Bytes(),
				&types.TransactionFee{}, // transaction fee is not yet used
				types.SquareSize,
			)
			if err != nil {
				return err
			}

			// sign the PayForMessage's ShareCommitments
			err = pfmMsg.SignShareCommitments(accName, clientCtx.Keyring)
			if err != nil {
				return err
			}

			// run message checks
			if err = pfmMsg.ValidateBasic(); err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), pfmMsg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
