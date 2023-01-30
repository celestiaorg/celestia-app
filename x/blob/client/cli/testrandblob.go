package cli

import (
	"fmt"
	"strconv"

	"github.com/celestiaorg/celestia-app/testutil/namespace"
	"github.com/celestiaorg/celestia-app/testutil/testfactory"
	"github.com/celestiaorg/celestia-app/x/blob/types"

	"github.com/spf13/cobra"

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
			// decode the blob size
			size, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("failure to decode blob size: %w", err)
			}

			nid := namespace.RandomBlobNamespace()
			coreBlob := testfactory.GenerateBlobsWithNamespace(1, size, nid)
			blob, err := types.NewBlob(coreBlob[0].NamespaceID, coreBlob[0].Data)
			if err != nil {
				return fmt.Errorf("failure on generating random blob: %w", err)
			}

			return broadcastPFB(cmd, blob)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
