package cli

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/input"
	sdktx "github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// FlagNamespaceVersion specifies namespace version when user submit a pay for blob.
const FlagNamespaceVersion = "namespace-version"

func CmdPayForBlob() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "PayForBlobs [hexNamespaceID] [hexBlob]",
		Short: "Pay for a data blob to be published to the Celestia blockchain",
		Long: "Pay for a data blob to be published to the Celestia blockchain. " +
			"[hexNamespaceID] must be a 10 byte hex encoded namespace ID. " +
			"[hexBlob] can be an arbitrary length hex encoded data blob. " +
			"This command only supports a single blob per invocation. ",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			namespaceID, err := hex.DecodeString(args[0])
			if err != nil {
				return fmt.Errorf("failure to decode hex namespace ID: %w", err)
			}

			namespaceFlag, _ := cmd.Flags().GetString(FlagNamespaceVersion)
			var namespace appns.Namespace

			if namespaceFlag != "" {
				// load given namespace version
				namespaceflag, err := strconv.Atoi(namespaceFlag)
				if err != nil {
					return fmt.Errorf("failure to convert namespace from string: %w", err)
				}
				if uint8(namespaceflag) != appns.NamespaceVersionZero {
					return fmt.Errorf("unsupported namespace version %v", namespaceflag)
				}
				namespace, err = appns.New(appns.NamespaceVersionZero, append(appns.NamespaceVersionZeroPrefix, namespaceID...))
				if err != nil {
					return fmt.Errorf("failure to create namespace: %w", err)
				}
			} else {
				// namespace version should default to the latest supported namespace version.
				namespace, err = appns.New(appns.NamespaceVersionZero, append(appns.NamespaceVersionZeroPrefix, namespaceID...))
				if err != nil {
					return fmt.Errorf("failure to create namespace: %w", err)
				}
			}

			rawblob, err := hex.DecodeString(args[1])
			if err != nil {
				return fmt.Errorf("failure to decode hex blob: %w", err)
			}

			// TODO: allow for more than one blob to be sumbmitted via the cli
			blob, err := types.NewBlob(namespace, rawblob)
			if err != nil {
				return err
			}

			return broadcastPFB(cmd, blob)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	cmd.PersistentFlags().String(FlagNamespaceVersion, "", "User can use this to specify the namespace version when they submit a pay for blob.")

	return cmd
}

// broadcastPFB creates the new PFB message type that will later be broadcast to tendermint nodes
// this private func is used in CmdPayForBlob and CmdTestRandBlob
func broadcastPFB(cmd *cobra.Command, blob *types.Blob) error {
	clientCtx, err := client.GetClientTxContext(cmd)
	if err != nil {
		return err
	}

	// TODO: allow the user to override the share version via a new flag
	// See https://github.com/celestiaorg/celestia-app/issues/1041
	pfbMsg, err := types.NewMsgPayForBlobs(clientCtx.FromAddress.String(), blob)
	if err != nil {
		return err
	}

	// run message checks
	if err = pfbMsg.ValidateBasic(); err != nil {
		return err
	}

	txBytes, err := writeTx(clientCtx, sdktx.NewFactoryCLI(clientCtx, cmd.Flags()), pfbMsg)
	if err != nil {
		return err
	}

	blobTx, err := coretypes.MarshalBlobTx(txBytes, blob)
	if err != nil {
		return err
	}

	// broadcast to a Tendermint node
	res, err := clientCtx.BroadcastTx(blobTx)
	if err != nil {
		return err
	}

	return clientCtx.PrintProto(res)
}

// writeTx attempts to generate and sign a transaction using the normal
// cosmos-sdk cli argument parsing code with the given set of messages. It will also simulate gas
// requirements if necessary. It will return an error upon failure.
//
// NOTE: Copy paste forked from the cosmos-sdk so that we can wrap the PFB with
// a blob while still using all of the normal cli parsing code
func writeTx(clientCtx client.Context, txf sdktx.Factory, msgs ...sdk.Msg) ([]byte, error) {
	if clientCtx.GenerateOnly {
		return nil, txf.PrintUnsignedTx(clientCtx, msgs...)
	}

	txf, err := txf.Prepare(clientCtx)
	if err != nil {
		return nil, err
	}

	if txf.SimulateAndExecute() || clientCtx.Simulate {
		_, adjusted, err := sdktx.CalculateGas(clientCtx, txf, msgs...)
		if err != nil {
			return nil, err
		}

		txf = txf.WithGas(adjusted)
		_, _ = fmt.Fprintf(os.Stderr, "%s\n", sdktx.GasEstimateResponse{GasEstimate: txf.Gas()})
	}

	if clientCtx.Simulate {
		return nil, nil
	}

	tx, err := txf.BuildUnsignedTx(msgs...)
	if err != nil {
		return nil, err
	}

	if !clientCtx.SkipConfirm {
		txBytes, err := clientCtx.TxConfig.TxJSONEncoder()(tx.GetTx())
		if err != nil {
			return nil, err
		}

		if err := clientCtx.PrintRaw(json.RawMessage(txBytes)); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "%s\n", txBytes)
		}

		buf := bufio.NewReader(os.Stdin)
		ok, err := input.GetConfirmation("confirm transaction before signing and broadcasting", buf, os.Stderr)

		if err != nil || !ok {
			_, _ = fmt.Fprintf(os.Stderr, "%s\n", "cancelled transaction")
			return nil, err
		}
	}

	err = sdktx.Sign(txf, clientCtx.GetFromName(), tx, true)
	if err != nil {
		return nil, err
	}

	return clientCtx.TxConfig.TxEncoder()(tx.GetTx())
}
