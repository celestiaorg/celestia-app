package shares

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/testutil/testfactory"
	"github.com/celestiaorg/rsmt2d"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

func TestFuzz_reconstruct(t *testing.T) {
	t.Skip()
	// run random shares through processCompactShares for a minute
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			Test_reconstruct_randomData(t)
		}
	}
}

func Test_reconstruct_randomData(t *testing.T) {
	type test struct {
		name      string
		txCount   int
		blobCount int
		maxSize   int // max size of each tx or blob
	}

	tests := []test{
		{"one of each random small size", 1, 1, 40},
		{"one of each random large size", 1, 1, 400},
		{"many of each random large size", 10, 10, 40},
		{"many of each random large size", 10, 10, 400},
		{"only transactions", 10, 0, 400},
		{"only blobs", 0, 10, 400},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := generateRandomBlockData(
				tc.txCount,
				tc.blobCount,
				tc.maxSize,
			)

			shares, err := Split(data, false)
			require.NoError(t, err)

			eds, err := rsmt2d.ComputeExtendedDataSquare(ToBytes(shares), appconsts.DefaultCodec(), rsmt2d.NewDefaultTree)
			assert.NoError(t, err)

			got, err := reconstruct(eds)
			assert.NoError(t, err)
			assert.Equal(t, data, got)
		})
	}
}

func Test_reconstruct_sampleBlock(t *testing.T) {
	var pb tmproto.Block
	err := json.Unmarshal([]byte(sampleBlock), &pb)
	require.NoError(t, err)

	b, err := coretypes.BlockFromProto(&pb)
	require.NoError(t, err)

	shares, err := Split(b.Data, false)
	require.NoError(t, err)

	eds, err := rsmt2d.ComputeExtendedDataSquare(ToBytes(shares), appconsts.DefaultCodec(), rsmt2d.NewDefaultTree)
	assert.NoError(t, err)

	got, err := reconstruct(eds)
	got.Hash()

	// TODO although the Txs are identical, the hashes don't match
	fmt.Printf("data.Txs: %x\n", b.Data.Txs)
	fmt.Printf("data.Hash(): %x\n", b.Data.Hash())
	fmt.Printf("got: %x\n", got.Txs)
	fmt.Printf("got.Hash(): %x\n", got.Hash())

	assert.NoError(t, err)
	assert.Equal(t, got, b.Data)
}

// generateRandomBlockData returns randomly generated block data for testing purposes
func generateRandomBlockData(txCount, blobCount, maxSize int) (data coretypes.Data) {
	data.Txs = testfactory.GenerateRandomlySizedTxs(txCount, maxSize)
	data.Blobs = testfactory.GenerateRandomlySizedBlobs(blobCount, maxSize)
	data.SquareSize = appconsts.DefaultMaxSquareSize
	return data
}

// this is a sample block
//
//go:embed "testdata/sample-block.json"
var sampleBlock string
