package cli

import (
	"bytes"
	"fmt"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/nmt/namespace"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/client/flags"
)

// CmdTestRandBlob is triggered by testground's tests as part of apps' node scenario
// to increase the block size by user-defined amount.
//
// CAUTION: This func should not be used in production env!
func CmdTestRandBlob() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "TestRandBlob [blobSize]",
		Short: "Generates a random blob for a random namespace to be published to the Celestia blockchain",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// decode the namespace
			namespace := getRandomNamespace()

			// decode the message
			size, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("failure to decode message size: %w", err)
			}

			// decode the blob
			rawblob := getRandomBlobBySize(size)

			// TODO: allow for more than one blob to be sumbmitted via the cli
			blob, err := types.NewBlob(namespace, rawblob)
			if err != nil {
				return err
			}

			return broadcastPFB(cmd, blob)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func getRandomNamespace() namespace.ID {
	for {
		s := tmrand.Bytes(8)
		if bytes.Compare(s, appconsts.MaxReservedNamespace) > 0 {
			return s
		}
	}
}

func getRandomBlobBySize(size int) []byte {
	return tmrand.Bytes(size)
}
