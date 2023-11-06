package app_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/da"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/pkg/square"
	"github.com/stretchr/testify/require"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"
	"github.com/tendermint/tendermint/rpc/client/http"
)

func TestDebug(t *testing.T) {
	client, err := http.New("http://consensus-validator-arabica-9.celestia-arabica.com:26657", "/websocket")
	require.NoError(t, err)

	h := int64(743175)
	ph := &h

	bres, err := client.Block(context.Background(), ph)
	require.NoError(t, err)

	var byteTxs [][]byte
	for _, tx := range bres.Block.Txs {
		byteTxs = append(byteTxs, tx)
	}

	err = processProp(byteTxs, bres.Block.SquareSize, bres.Block.DataHash)
	require.NoError(t, err)
	fmt.Println("finished test", err)
}

func processProp(txs [][]byte, squareSize uint64, dataRoot []byte) error {
	fmt.Println("running process prop")
	// Construct the data square from the block's transactions
	dataSquare, err := square.Construct(txs, 1, 64)
	if err != nil {
		return err
	}

	// Assert that the square size stated by the proposer is correct
	if uint64(dataSquare.Size()) != squareSize {
		return err
	}
	fmt.Println("computing data root")
	eds, err := da.ExtendShares(shares.ToBytes(dataSquare))
	if err != nil {
		return err
	}

	dah := da.NewDataAvailabilityHeader(eds)
	// by comparing the hashes we know the computed IndexWrappers (with the share indexes of the PFB's blobs)
	// are identical and that square layout is consistent. This also means that the share commitment rules
	// have been followed and thus each blobs share commitment should be valid
	if !bytes.Equal(dah.Hash(), dataRoot) {
		return fmt.Errorf("proposed data root differs from calculated data root: %s %s", tmbytes.HexBytes(dah.Hash()), tmbytes.HexBytes(dataRoot))
	}

	fmt.Println("computed data root", tmbytes.HexBytes(dah.Hash()))

	return nil
}
