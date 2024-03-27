package cli

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

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

const (
	// FlagShareVersion allows the user to override the share version when
	// submitting a PayForBlob.
	FlagShareVersion = "share-version"

	// FlagNamespaceVersion allows the user to override the namespace version when
	// submitting a PayForBlob.
	FlagNamespaceVersion = "namespace-version"
)

func CmdPayForBlob() *cobra.Command {
	cmd := &cobra.Command{
		Use: "PayForBlobs namespaceID blob",
		// This example command can be run in a new terminal after running single-node.sh
		Example: "celestia-appd tx blob PayForBlobs 0x00010203040506070809 0x48656c6c6f2c20576f726c6421 \\\n" +
			"\t--chain-id private \\\n" +
			"\t--from validator \\\n" +
			"\t--keyring-backend test \\\n" +
			"\t--fees 21000utia \\\n" +
			"\t--yes",
		Short: "Pay for a data blob to be published to Celestia.",
		Long: "Pay for a data blob to be published to Celestia.\n" +
			"namespaceID is the user-specifiable portion of a version 0 namespace. It must be a hex encoded string of 10 bytes.\n" +
			"blob must be a hex encoded string of any length.\n" +
			// TODO: allow for more than one blob to be sumbmitted via the CLI
			"This command currently only supports a single blob per invocation.\n",
		Aliases: []string{"PayForBlob"},
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("PayForBlobs requires two arguments: namespaceID and blob")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			arg0 := strings.TrimPrefix(args[0], "0x")
			namespaceID, err := hex.DecodeString(arg0)
			if err != nil {
				return fmt.Errorf("failed to decode hex namespace ID: %w", err)
			}
			namespaceVersion, err := cmd.Flags().GetUint8(FlagNamespaceVersion)
			if err != nil {
				return err
			}
			namespace, err := getNamespace(namespaceID, namespaceVersion)
			if err != nil {
				return err
			}

			arg1 := strings.TrimPrefix(args[1], "0x")
			rawblob, err := hex.DecodeString(arg1)
			if err != nil {
				return fmt.Errorf("failure to decode hex blob: %w", err)
			}

			shareVersion, _ := cmd.Flags().GetUint8(FlagShareVersion)
			blob, err := types.NewBlob(namespace, rawblob, shareVersion)
			if err != nil {
				return err
			}

			return broadcastPFB(cmd, blob)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	cmd.PersistentFlags().Uint8(FlagNamespaceVersion, 0, "Specify the namespace version (default 0)")
	cmd.PersistentFlags().Uint8(FlagShareVersion, 0, "Specify the share version (default 0)")
	_ = cmd.MarkFlagRequired(flags.FlagFrom)
	return cmd
}

func getNamespace(namespaceID []byte, namespaceVersion uint8) (appns.Namespace, error) {
	switch namespaceVersion {
	case appns.NamespaceVersionZero:
		if len(namespaceID) != appns.NamespaceVersionZeroIDSize {
			return appns.Namespace{}, fmt.Errorf("the user specifiable portion of the namespace ID must be %d bytes for namespace version 0", appns.NamespaceVersionZeroIDSize)
		}
		id := make([]byte, 0, appns.NamespaceIDSize)
		id = append(id, appns.NamespaceVersionZeroPrefix...)
		id = append(id, namespaceID...)
		return appns.New(namespaceVersion, id)
	default:
		return appns.Namespace{}, fmt.Errorf("namespace version %d is not supported", namespaceVersion)
	}
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
