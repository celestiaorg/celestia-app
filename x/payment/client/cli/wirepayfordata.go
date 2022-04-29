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

const FlagSquareSizes = "square-sizes"

func CmdWirePayForData() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "payForData [hexNamespace] [hexMessage]",
		Short: "Creates a new MsgWirePayForData",
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

			// create the MsgPayForData
			squareSizes, err := cmd.Flags().GetUintSlice(FlagSquareSizes)
			if err != nil {
				return err
			}
			squareSizes64 := parseSquareSizes(squareSizes)
			pfdMsg, err := types.NewWirePayForData(namespace, message, squareSizes64...)
			if err != nil {
				return err
			}

			// use the keyring to programmatically sign multiple PayForData txs
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

			// sign the  MsgPayForData's ShareCommitments
			err = pfdMsg.SignShareCommitments(
				signer,
				types.SetGasLimit(gasSetting.Gas),
				types.SetFeeAmount(parsedFees),
			)
			if err != nil {
				return err
			}

			// run message checks
			if err = pfdMsg.ValidateBasic(); err != nil {
				return err
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), pfdMsg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	cmd.Flags().UintSlice(FlagSquareSizes, []uint{consts.MaxSquareSize, 128, 64}, "Specify the square sizes, must be power of 2")

	return cmd
}

func parseSquareSizes(squareSizes []uint) []uint64 {
	squareSizes64 := make([]uint64, len(squareSizes))
	for i := range squareSizes {
		squareSizes64[i] = uint64(squareSizes[i])
	}
	return squareSizes64
}
