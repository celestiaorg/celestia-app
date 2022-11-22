package prove

import (
	"math/rand"
	"sort"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/testutil/namespace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/types"
)

func TestTxInclusion(t *testing.T) {
	typicalBlockData := types.Data{
		Txs:        generateRandomlySizedTxs(100, 500),
		Blobs:      generateRandomlySizedBlobs(40, 16000),
		SquareSize: 64,
	}
	lotsOfTxsNoMessages := types.Data{
		Txs:        generateRandomlySizedTxs(1000, 500),
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
	overlappingRowsBlockDataWithMessages := types.Data{
		Txs: types.ToTxs(
			[][]byte{
				tmrand.Bytes(appconsts.ContinuationCompactShareContentSize*overlappingSquareSize + 1),
				tmrand.Bytes(10000),
			},
		),
		Blobs:      generateRandomlySizedBlobs(8, 400),
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
			lotsOfTxsNoMessages,
		},
		{
			overlappingRowsBlockData,
		},
		{
			overlappingRowsBlockDataWithMessages,
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
			txs:  generateRandomlySizedTxs(44, 200),
		},
		{
			name: "many small tx",
			txs:  generateRandomlySizedTxs(444, 100),
		},
		{
			// this is a concrete output from generateRandomlySizedTxs(444, 100)
			// that surfaced a bug in txSharePositions so it is included here to
			// prevent regressions
			name: "many small tx (without randomness)",
			txs:  manySmallTxsWithoutRandomness,
		},
		{
			name: "one small tx",
			txs:  generateRandomlySizedTxs(1, 200),
		},
		{
			name: "one large tx",
			txs:  generateRandomlySizedTxs(1, 2000),
		},
		{
			name: "many large txs",
			txs:  generateRandomlySizedTxs(100, 2000),
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
			rawTxDataForRange := stripCompactShares(shares, pos.start, pos.end)
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
		// 81 full compact shares then a full 82nd share
		{appconsts.FirstCompactShareContentSize + (appconsts.ContinuationCompactShareContentSize * 80) + 501, 81},
		// 82 full compact shares then one byte in 83rd share
		{appconsts.FirstCompactShareContentSize + (appconsts.ContinuationCompactShareContentSize * 80) + 502, 82},
		// 82 compact shares then two bytes in 83rd share
		{appconsts.FirstCompactShareContentSize + (appconsts.ContinuationCompactShareContentSize * 80) + 503, 82},
	}

	for _, tt := range tests {
		got := txShareIndex(tt.totalTxLen)
		if got != tt.wantIndex {
			t.Errorf("txShareIndex(%d) got %d, want %d", tt.totalTxLen, got, tt.wantIndex)
		}
	}
}

// stripCompactShares strips the universal prefix (namespace, info byte, data length) and
// reserved byte from a list of compact shares and joins them into a single byte
// slice.
func stripCompactShares(compactShares []shares.Share, start uint64, end uint64) (result []byte) {
	for i := start; i <= end; i++ {
		if i == 0 {
			// the first compact share includes a total sequence length varint
			result = append(result, compactShares[i][appconsts.NamespaceSize+appconsts.ShareInfoBytes+appconsts.FirstCompactShareSequenceLengthBytes+appconsts.CompactShareReservedBytes:]...)
		} else {
			result = append(result, compactShares[i][appconsts.NamespaceSize+appconsts.ShareInfoBytes+appconsts.CompactShareReservedBytes:]...)
		}
	}
	return result
}

func generateRandomlySizedTxs(count, max int) types.Txs {
	txs := make(types.Txs, count)
	for i := 0; i < count; i++ {
		size := rand.Intn(max)
		if size == 0 {
			size = 1
		}
		txs[i] = generateRandomTxs(1, size)[0]
	}
	return txs
}

func generateRandomTxs(count, size int) types.Txs {
	txs := make(types.Txs, count)
	for i := 0; i < count; i++ {
		tx := make([]byte, size)
		_, err := rand.Read(tx)
		if err != nil {
			panic(err)
		}
		txs[i] = tx
	}
	return txs
}

func generateRandomlySizedBlobs(count, maxMsgSize int) []types.Blob {
	blobs := make([]types.Blob, count)
	for i := 0; i < count; i++ {
		blobs[i] = generateRandomBlob(rand.Intn(maxMsgSize))
	}

	// this is just to let us use assert.Equal
	if count == 0 {
		blobs = nil
	}

	sort.Sort(types.BlobsByNamespace(blobs))
	return blobs
}

func generateRandomBlob(size int) types.Blob {
	blob := types.Blob{
		NamespaceID: namespace.RandomMessageNamespace(),
		Data:        tmrand.Bytes(size),
	}
	return blob
}
