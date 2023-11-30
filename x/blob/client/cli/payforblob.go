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

	"github.com/celestiaorg/celestia-app/pkg/blob"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/x/blob/types"
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
)

func CmdPayForBlob() *cobra.Command {
	cmd := &cobra.Command{
		Use: "PayForBlobs [path/to/blob.json]",
		// This example command can be run in a new terminal after running single-node.sh
		Example: "celestia-appd tx blob PayForBlobs path/to/blob.json \\\n" +
			"\t--chain-id private \\\n" +
			"\t--from validator \\\n" +
			"\t--keyring-backend test \\\n" +
			"\t--fees 21000utia \\\n" +
			"\t--yes",
		Short: "Pay for a data blobs to be published to Celestia.",
		Long: "Pay for a data blobs to be published to Celestia.\n" +
			`Where blob.json contains: 

		{
			"Blobs": [
				{
					"namespaceId": "0x00010203040506070809",
					"blob": "0x48656c6c6f2c20576f726c6421"
				},
				{
					"namespaceId": "0x00010203040506070809",
					"blob": "0x48656c6c6f2c20576f726c6421"
				}
			]
		}

		namespaceID is the user-specifiable portion of a version 0 namespace. It must be a hex encoded string of 10 bytes.\n
		blob must be a hex encoded string of any length.\n
		
		`,
		Aliases: []string{"PayForBlob"},
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("PayForBlobs requires one arguments: path to blob.json")
			}
			path := args[0]
			if filepath.Ext(path) != ".json" {
				return fmt.Errorf("invalid file extension, require json got %s", filepath.Ext(path))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			namespaceVersion, err := cmd.Flags().GetUint8(FlagNamespaceVersion)
			path := args[0]

			paresdBlobs, err := parseSubmitBlobs(clientCtx.Codec, path)
			if err != nil {
				return err
			}

			var blobs []*blob.Blob
			for i := range paresdBlobs {
				namespaceID, err := hex.DecodeString(strings.TrimPrefix(paresdBlobs[i].NamespaceId, "0x"))
				if err != nil {
					return fmt.Errorf("failed to decode hex namespace ID: %w", err)
				}
				namespace, err := getNamespace(namespaceID, namespaceVersion)
				if err != nil {
					return err
				}
				hexStr := strings.TrimPrefix(paresdBlobs[i].Blob, "0x")
				rawblob, err := hex.DecodeString(hexStr)
				if err != nil {
					return fmt.Errorf("failure to decode hex blob value %s: %s", hexStr, err.Error())
				}

				shareVersion, _ := cmd.Flags().GetUint8(FlagShareVersion)
				blob, err := types.NewBlob(namespace, rawblob, shareVersion)
				if err != nil {
					return fmt.Errorf("failure to create blob with hex blob value %s: %s", hexStr, err.Error())

				}
				blobs = append(blobs, blob)
			}

			return broadcastPFB(cmd, blobs...)
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
// this private func is used in CmdPayForBlob
func broadcastPFB(cmd *cobra.Command, b ...*blob.Blob) error {
	clientCtx, err := client.GetClientTxContext(cmd)
	if err != nil {
		return err
	}

	// TODO: allow the user to override the share version via a new flag
	// See https://github.com/celestiaorg/celestia-app/issues/1041
	pfbMsg, err := types.NewMsgPayForBlobs(clientCtx.FromAddress.String(), b...)
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

	blobTx, err := blob.MarshalBlobTx(txBytes, b...)
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
