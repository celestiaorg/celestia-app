package app_test

import (
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/da"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/pkg/square"
	"github.com/stretchr/testify/require"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"
)

const (
	badRoot  = "257760461993F8F197B421EC7435F3C36C3734923E3DA9A42DC73B05F07B3D08"
	goodRoot = "3D96B7D238E7E0456F6AF8E7CDF0A67BD6CF9C2089ECB559C659DCAA1F880353"
)

var (
	badSquare = []byte{255, 255, 255, 255, 255, 255, 255, 254, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
)

func TestDebug(t *testing.T) {
	// client, err := http.New("tcp://rpc-2.consensus.celestia-arabica-10.com", "/websocket")
	// require.NoError(t, err)

	// h := int64(532423)
	// ph := &h

	// bres, err := client.Block(context.Background(), ph)
	// require.NoError(t, err)

	var byteTxs [][]byte
	// for _, tx := range bres.Block.Txs {
	// 	byteTxs = append(byteTxs, tx)
	// }

	err := processProp(byteTxs, 1, badRoot)
	require.NoError(t, err)
	fmt.Println("finished test", err)
}

func processProp(txs [][]byte, squareSize uint64, dataRoot string) error {
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

	dah, err := da.NewDataAvailabilityHeader(eds)
	if err != nil {
		return err
	}
	// by comparing the hashes we know the computed IndexWrappers (with the share indexes of the PFB's blobs)
	// are identical and that square layout is consistent. This also means that the share commitment rules
	// have been followed and thus each blobs share commitment should be valid
	if tmbytes.HexBytes(dah.Hash()).String() != dataRoot {
		return fmt.Errorf("proposed data root differs from calculated data root: %s %s", tmbytes.HexBytes(dah.Hash()), dataRoot)
	}

	fmt.Println("computed data root", tmbytes.HexBytes(dah.Hash()))

	return nil
}

// func oldProcessProp(data types.Data, squareSize uint64, dataRoot []byte) error {
// 	dataSquare, err := shares.Split(data, true)
// 	if err != nil {
// 		return err
// 	}

// 	cacher := wrapper.NewConstructor(squareSize)
// 	eds, err := rsmt2d.ComputeExtendedDataSquare(shares.ToBytes(dataSquare), appconsts.DefaultCodec(), cacher)
// 	if err != nil {
// 		return err
// 	}

// 	dah := da.NewDataAvailabilityHeader(eds)

// 	if !bytes.Equal(dah.Hash(), dataRoot) {
// 		return fmt.Errorf("proposed data root differs from calculated data root: %s %s", tmbytes.HexBytes(dah.Hash()), tmbytes.HexBytes(dataRoot))
// 	}

// 	fmt.Println(tmbytes.HexBytes(dah.Hash()))

// 	return nil

// }

func TestDebug2(t *testing.T) {
	// // for {
	// s, _, err := square.Build([][]byte{}, 1, 64)
	// require.NoError(t, err)

	// shs := shares.ToBytes(s)
	// fmt.Println(shs)

	// squareSize := uint64(1)

	// dataSquare, err := shares.Split(types.Data{SquareSize: uint64(squareSize)}, true)
	// require.NoError(t, err)

	// cacher := wrapper.NewConstructor(squareSize)
	// eds, err := rsmt2d.ComputeExtendedDataSquare(shares.ToBytes(dataSquare), appconsts.DefaultCodec(), cacher)
	// require.NoError(t, err)

	// // erasure the data square which we use to create the data root.
	// // Note: uses the nmt wrapper to construct the tree.
	// // checkout pkg/wrapper/nmt_wrapper.go for more information.
	eds, err := da.ExtendShares([][]byte{badSquare})
	require.NoError(t, err)

	// create the new data root by creating the data availability header (merkle
	// roots of each row and col of the erasure data).
	dah, err := da.NewDataAvailabilityHeader(eds)
	require.NoError(t, err)

	h := dah.Hash()

	require.Equal(t, badRoot, tmbytes.HexBytes(h).String())
	// }
}
