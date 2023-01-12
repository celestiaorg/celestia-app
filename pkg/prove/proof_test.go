package prove

import (
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/testutil/testfactory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/types"
)

func TestTxInclusion(t *testing.T) {
	typicalBlockData := types.Data{
		Txs:        testfactory.GenerateRandomlySizedTxs(100, 500),
		Blobs:      testfactory.GenerateRandomlySizedBlobs(40, 16000),
		SquareSize: 64,
	}
	lotsOfTxsNoBlobs := types.Data{
		Txs:        testfactory.GenerateRandomlySizedTxs(1000, 500),
		SquareSize: 64,
	}
	overlappingSquareSize := 16
	overlappingRowsBlockData := types.Data{
		Txs: types.ToTxs(
			[][]byte{
				tmrand.Bytes(appconsts.ContinuationCompactShareContentSize*overlappingSquareSize + 1),
				tmrand.Bytes(10000),
			},
		),
		SquareSize: uint64(overlappingSquareSize),
	}
	overlappingRowsBlockDataWithBlobs := types.Data{
		Txs: types.ToTxs(
			[][]byte{
				tmrand.Bytes(appconsts.ContinuationCompactShareContentSize*overlappingSquareSize + 1),
				tmrand.Bytes(10000),
			},
		),
		Blobs:      testfactory.GenerateRandomlySizedBlobs(8, 400),
		SquareSize: uint64(overlappingSquareSize),
	}

	type test struct {
		data types.Data
	}
	tests := []test{
		{
			typicalBlockData,
		},
		{
			lotsOfTxsNoBlobs,
		},
		{
			overlappingRowsBlockData,
		},
		{
			overlappingRowsBlockDataWithBlobs,
		},
	}

	for _, tt := range tests {
		for i := 0; i < len(tt.data.Txs); i++ {
			txProof, err := TxInclusion(appconsts.DefaultCodec(), tt.data, uint64(i))
			require.NoError(t, err)
			assert.True(t, txProof.VerifyProof())
		}
	}
}

func TestTxSharePosition(t *testing.T) {
	type test struct {
		name string
		txs  types.Txs
	}

	tests := []test{
		{
			name: "typical",
			txs:  testfactory.GenerateRandomlySizedTxs(44, 200),
		},
		{
			name: "many small tx",
			txs:  testfactory.GenerateRandomlySizedTxs(444, 100),
		},
		{
			// this is a concrete output from testfactory.GenerateRandomlySizedTxs(444, 100)
			// that surfaced a bug in txSharePositions so it is included here to
			// prevent regressions
			name: "many small tx (without randomness)",
			txs:  manySmallTxsWithoutRandomness,
		},
		{
			name: "one small tx",
			txs:  testfactory.GenerateRandomlySizedTxs(1, 200),
		},
		{
			name: "one large tx",
			txs:  testfactory.GenerateRandomlySizedTxs(1, 2000),
		},
		{
			name: "many large txs",
			txs:  testfactory.GenerateRandomlySizedTxs(100, 2000),
		},
	}

	type startEndPoints struct {
		start, end uint64
	}

	for _, tt := range tests {
		positions := make([]startEndPoints, len(tt.txs))
		for i := 0; i < len(tt.txs); i++ {
			start, end, err := txSharePosition(tt.txs, uint64(i))
			require.NoError(t, err)
			positions[i] = startEndPoints{start: start, end: end}
		}

		shares := shares.SplitTxs(tt.txs)

		for i, pos := range positions {
			rawTx := []byte(tt.txs[i])
			rawTxDataForRange, err := stripCompactShares(shares[pos.start : pos.end+1])
			assert.NoError(t, err)
			assert.Contains(
				t,
				string(rawTxDataForRange),
				string(rawTx),
				tt.name,
				pos,
				len(tt.txs[i]),
			)
		}
	}
}

func TestTxShareIndex(t *testing.T) {
	type testCase struct {
		totalTxLen int
		wantIndex  uint64
	}

	tests := []testCase{
		{0, 0},
		{10, 0},
		{100, 0},
		{appconsts.FirstCompactShareContentSize, 0},
		{appconsts.FirstCompactShareContentSize + 1, 1},
		{appconsts.FirstCompactShareContentSize + appconsts.ContinuationCompactShareContentSize, 1},
		{appconsts.FirstCompactShareContentSize + appconsts.ContinuationCompactShareContentSize + 1, 2},
		{appconsts.FirstCompactShareContentSize + (appconsts.ContinuationCompactShareContentSize * 2), 2},
		{appconsts.FirstCompactShareContentSize + (appconsts.ContinuationCompactShareContentSize * 2) + 1, 3},
		// 81 full compact shares then a partially filled out 82nd share (which is index 81 because 0-indexed)
		{appconsts.FirstCompactShareContentSize + (appconsts.ContinuationCompactShareContentSize * 80) + 160, 81},
		// 82 full compact shares
		{appconsts.FirstCompactShareContentSize + (appconsts.ContinuationCompactShareContentSize * 81), 81},
		// 82 full compact shares then one byte in 83rd share
		{appconsts.FirstCompactShareContentSize + (appconsts.ContinuationCompactShareContentSize * 81) + 1, 82},
		// 82 compact shares then two bytes in 83rd share
		{appconsts.FirstCompactShareContentSize + (appconsts.ContinuationCompactShareContentSize * 81) + 2, 82},
	}

	for _, tt := range tests {
		got := txShareIndex(tt.totalTxLen)
		if got != tt.wantIndex {
			t.Errorf("txShareIndex(%d) got %d, want %d", tt.totalTxLen, got, tt.wantIndex)
		}
	}
}

// stripCompactShares strips the universal prefix (namespace, info byte, sequence length) and
// reserved bytes from a list of compact shares and joins them into a single byte
// slice.
func stripCompactShares(compactShares []shares.Share) (result []byte, err error) {
	for _, compactShare := range compactShares {
		rawData, err := compactShare.RawData()
		if err != nil {
			return []byte{}, err
		}
		result = append(result, rawData...)
	}
	return result, nil
}
