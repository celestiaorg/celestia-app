package shares

import (
	"context"
	_ "embed"
	"encoding/json"
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
	assert.NoError(t, err)

	// TODO: the assertions below are a hack because the data returned by reconstruct does
	// contain the same hash as the original block. Ideally this test would verify:
	//
	//     assert.Equal(t, got, b.Data)
	//
	// Instead this test verifies all public fields of Data are identical.
	assert.Equal(t, got.Txs, b.Data.Txs)
	assert.Equal(t, got.Blobs, b.Data.Blobs)
	assert.Equal(t, got.SquareSize, b.Data.SquareSize)
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
