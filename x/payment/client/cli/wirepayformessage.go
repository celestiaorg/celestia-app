package cli

import (
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tendermint/tendermint/pkg/consts"

	"github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func CmdWirePayForMessage() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "payForMessage [hexNamespace] [hexMessage]",
		Short: "Creates a new MsgWirePayForMessage",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			// query for account number
			fromAddress := clientCtx.GetFromAddress()
			account, err := clientCtx.AccountRetriever.GetAccount(clientCtx, fromAddress)
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

			// decode the message
			message, err := hex.DecodeString(args[1])
			if err != nil {
				return fmt.Errorf("failure to decode hex message: %w", err)
			}

			// create the MsgPayForMessage
			pfmMsg, err := types.NewWirePayForMessage(namespace, message, consts.MaxSquareSize)
			if err != nil {
				return err
			}

			// use the keyring to programmatically sign multiple PayForMessage txs
			signer := types.NewKeyringSigner(clientCtx.Keyring, accName, clientCtx.ChainID)

			signer.SetAccountNumber(account.GetAccountNumber())
			signer.SetSequence(account.GetSequence())

			// get and parse the gas limit for this tx
			rawGasLimit, err := cmd.Flags().GetString(flags.FlagGas)
			if err != nil {
				return err
			}
			gasSetting, err := flags.ParseGasSetting(rawGasLimit)
			if err != nil {
				return err
			}

			// get and parse the fees for this tx
			fees, err := cmd.Flags().GetString(flags.FlagFees)
			if err != nil {
				return err
			}
			parsedFees, err := sdk.ParseCoinsNormalized(fees)
			if err != nil {
				return err
			}

			// sign the  MsgPayForMessage's ShareCommitments
			err = pfmMsg.SignShareCommitments(
				signer,
				types.SetGasLimit(gasSetting.Gas),
				types.SetFeeAmount(parsedFees),
			)
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
