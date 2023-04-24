package shares

import (
	"context"
	_ "embed"
	"encoding/json"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	"github.com/celestiaorg/rsmt2d"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

func TestFuzz_merge(t *testing.T) {
	t.Skip()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			Test_merge_randomData(t)
		}
	}
}

func Test_merge_randomData(t *testing.T) {
	type test struct {
		name      string
		txCount   int
		blobCount int
		maxSize   int // max size of each tx or blob
	}

	tests := []test{
		{"just one tx", 1, 0, 40},
		{"just one blob", 0, 1, 40},
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

			got, err := merge(eds)
			assert.NoError(t, err)
			assert.Equal(t, data, got)
		})
	}
}

// Test_merge_sampleBlock verifies the behavior of merge against a sampleBlock.
// To update the sampleBlock see:
// https://github.com/celestiaorg/celestia-node/blob/a27f8488a4d732b00a9ae2ff8b212040111151c7/state/integration_test.go#L119-L121
func Test_merge_sampleBlock(t *testing.T) {
	var pb tmproto.Block
	err := json.Unmarshal([]byte(sampleBlock), &pb)
	require.NoError(t, err)

	b, err := coretypes.BlockFromProto(&pb)
	require.NoError(t, err)

	shares, err := Split(b.Data, false)
	require.NoError(t, err)

	eds, err := rsmt2d.ComputeExtendedDataSquare(ToBytes(shares), appconsts.DefaultCodec(), rsmt2d.NewDefaultTree)
	assert.NoError(t, err)

	got, err := merge(eds)
	assert.NoError(t, err)

	// TODO: the assertions below are a hack because the data returned by merge
	// does not contain the same hash as the original block. Ideally this test
	// would verify:
	//
	//     assert.Equal(t, b.Data, got)
	//
	// Instead this test verifies all public fields of Data are identical.
	assert.Equal(t, b.Data.Txs, got.Txs)
	assert.Equal(t, b.Data.Blobs, got.Blobs)
	assert.Equal(t, b.Data.SquareSize, got.SquareSize)
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
