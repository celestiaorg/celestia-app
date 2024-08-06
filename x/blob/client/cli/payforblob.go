package cli

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/x/blob/types"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/celestiaorg/go-square/v2/tx"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/input"
	sdktx "github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// FlagShareVersion allows the user to override the share version when
	// submitting a PayForBlob.
	FlagShareVersion = "share-version"

	// FlagNamespaceVersion allows the user to override the namespace version when
	// submitting a PayForBlob.
	FlagNamespaceVersion = "namespace-version"

	// FlagFileInput allows the user to provide the path to a JSON file for
	// submitting multiple blobs.
	FlagFileInput = "input-file"

	// FileInputExtension is the only file extension supported for
	// FlagFileInput.
	FileInputExtension = ".json"
)

func CmdPayForBlob() *cobra.Command {
	cmd := &cobra.Command{
		Use: "pay-for-blob [namespaceID blob]",
		Example: "celestia-appd tx blob pay-for-blob 0x00010203040506070809 0x48656c6c6f2c20576f726c6421 \\\n" +
			"\t--chain-id private \\\n" +
			"\t--from validator \\\n" +
			"\t--keyring-backend test \\\n" +
			"\t--fees 21000utia \\\n" +
			"\t--yes \n\n" +
			"celestia-appd tx blob pay-for-blob --input-file path/to/blobs.json \\\n" +
			"\t--chain-id private \\\n" +
			"\t--from validator \\\n" +
			"\t--keyring-backend test \\\n" +
			"\t--fees 21000utia \\\n" +
			"\t--yes \n",
		Short: "Pay for data blob(s) to be published to Celestia.",
		Long: `Pay for data blob(s) to be published to Celestia.
To publish a single blob, specify the namespaceID and blob via CLI arguments.
To publish multiple blobs, use the --input-file flag with the path to a JSON file.
The JSON should look like:

{
	"Blobs": [
		{
			"namespaceID": "0x00010203040506070809",
			"blob": "0x48656c6c6f2c20576f726c6421"
		},
		{
			"namespaceID": "0x00010203040506070809",
			"blob": "0x48656c6c6f2c20576f726c6421"
		}
	]
}

The namespaceID is the user-specifiable portion of a version 0 namespace.
The namespaceID must be a hex encoded string of 10 bytes.
The blob must be a hex encoded string of non-zero length.
		`,
		Aliases: []string{"pay-for-blobs", "PayForBlobs", "PayForBlob"},
		Args: func(cmd *cobra.Command, args []string) error {
			path, err := cmd.Flags().GetString(FlagFileInput)
			if err != nil {
				return err
			}

			if path != "" {
				if filepath.Ext(path) != FileInputExtension {
					return fmt.Errorf("invalid file extension %v. The only supported extension is %s", filepath.Ext(path), FileInputExtension)
				}

				return nil
			}

			if len(args) < 2 {
				return fmt.Errorf("pay-for-blob requires two arguments if %s isn't provided: namespaceID and blob", FlagFileInput)
			}

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			namespaceVersion, err := cmd.Flags().GetUint8(FlagNamespaceVersion)
			if err != nil {
				return err
			}

			shareVersion, err := cmd.Flags().GetUint8(FlagShareVersion)
			if err != nil {
				return err
			}

			path, err := cmd.Flags().GetString(FlagFileInput)
			if err != nil {
				return err
			}

			// In case of no file input, get the namespaceID and blob from the arguments
			if path == "" {
				blob, err := getBlobFromArguments(args[0], args[1], namespaceVersion, shareVersion)
				if err != nil {
					return err
				}

				return broadcastPFB(cmd, blob)
			}

			paresdBlobs, err := parseSubmitBlobs(path)
			if err != nil {
				return err
			}

			var blobs []*share.Blob
			for _, paresdBlob := range paresdBlobs {
				blob, err := getBlobFromArguments(paresdBlob.NamespaceID, paresdBlob.Blob, namespaceVersion, shareVersion)
				if err != nil {
					return err
				}
				blobs = append(blobs, blob)
			}

			return broadcastPFB(cmd, blobs...)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	cmd.PersistentFlags().Uint8(FlagNamespaceVersion, 0, "Specify the namespace version (default 0)")
	cmd.PersistentFlags().Uint8(FlagShareVersion, 0, "Specify the share version (default 0)")
	cmd.PersistentFlags().String(FlagFileInput, "", "Specify the file input")
	_ = cmd.MarkFlagRequired(flags.FlagFrom)
	return cmd
}

func getBlobFromArguments(namespaceIDArg, blobArg string, namespaceVersion, shareVersion uint8) (*share.Blob, error) {
	namespaceID, err := hex.DecodeString(strings.TrimPrefix(namespaceIDArg, "0x"))
	if err != nil {
		return nil, fmt.Errorf("failed to decode hex namespace ID: %w", err)
	}
	namespace, err := getNamespace(namespaceID, namespaceVersion)
	if err != nil {
		return nil, err
	}
	hexStr := strings.TrimPrefix(blobArg, "0x")
	rawblob, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, fmt.Errorf("failure to decode hex blob value %s: %s", hexStr, err.Error())
	}

	blob, err := types.NewBlob(namespace, rawblob, shareVersion)
	if err != nil {
		return nil, fmt.Errorf("failure to create blob with hex blob value %s: %s", hexStr, err.Error())
	}

	return blob, nil
}

func getNamespace(namespaceID []byte, namespaceVersion uint8) (share.Namespace, error) {
	switch namespaceVersion {
	case share.NamespaceVersionZero:
		if len(namespaceID) != share.NamespaceVersionZeroIDSize {
			return share.Namespace{}, fmt.Errorf("the user specifiable portion of the namespace ID must be %d bytes for namespace version 0", share.NamespaceVersionZeroIDSize)
		}
		id := make([]byte, 0, share.NamespaceIDSize)
		id = append(id, share.NamespaceVersionZeroPrefix...)
		id = append(id, namespaceID...)
		return share.NewNamespace(namespaceVersion, id)
	default:
		return share.Namespace{}, fmt.Errorf("namespace version %d is not supported", namespaceVersion)
	}
}

// broadcastPFB creates the new PFB message type that will later be broadcast to tendermint nodes
// this private func is used in CmdPayForBlob
func broadcastPFB(cmd *cobra.Command, b ...*share.Blob) error {
	clientCtx, err := client.GetClientTxContext(cmd)
	if err != nil {
		return err
	}

	// TODO: allow the user to override the share version via a new flag
	// See https://github.com/celestiaorg/celestia-app/issues/1041
	pfbMsg, err := types.NewMsgPayForBlobs(clientCtx.FromAddress.String(), appconsts.LatestVersion, b...)
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

	blobTx, err := tx.MarshalBlobTx(txBytes, b...)
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
