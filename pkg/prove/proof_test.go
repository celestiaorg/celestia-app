package prove

import (
	"sort"
	"testing"

	"github.com/celestiaorg/celestia-app/testutil/testfactory"

	"github.com/celestiaorg/celestia-app/pkg/da"
	nmtnamespace "github.com/celestiaorg/nmt/namespace"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/shares"
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

func TestShareInclusion(t *testing.T) {
	blobs := append(
		testfactory.GenerateBlobsWithNamespace(
			100,
			500,
			[]byte{0, 0, 0, 0, 0, 1, 0, 0},
		),
		append(
			testfactory.GenerateBlobsWithNamespace(
				50,
				500,
				[]byte{0, 0, 0, 1, 0, 0, 0, 0},
			),
			testfactory.GenerateBlobsWithNamespace(
				50,
				500,
				[]byte{0, 0, 1, 0, 0, 0, 0, 0},
			)...,
		)...,
	)
	sort.Sort(blobs)
	blockData := types.Data{
		Txs:        testfactory.GenerateRandomTxs(50, 500),
		Blobs:      blobs,
		SquareSize: 32,
	}

	// not setting useShareIndexes because the transactions indexes do not refer
	// to the messages because the square and transactions were created manually.
	rawShares, err := shares.Split(blockData, false)
	if err != nil {
		panic(err)
	}

	// erasure the data square which we use to create the data root.
	eds, err := da.ExtendShares(blockData.SquareSize, shares.ToBytes(rawShares))
	require.NoError(t, err)

	// create the new data root by creating the data availability header (merkle
	// roots of each row and col of the erasure data).
	dah := da.NewDataAvailabilityHeader(eds)
	dataRoot := dah.Hash()

	type test struct {
		name          string
		startingShare int64
		endingShare   int64
		namespaceID   nmtnamespace.ID
		expectErr     bool
	}
	tests := []test{
		{
			name:          "negative starting share",
			startingShare: -1,
			endingShare:   99,
			namespaceID:   appconsts.TxNamespaceID,
			expectErr:     true,
		},
		{
			name:          "negative ending share",
			startingShare: 0,
			endingShare:   -99,
			namespaceID:   appconsts.TxNamespaceID,
			expectErr:     true,
		},
		{
			name:          "ending share lower than starting share",
			startingShare: 1,
			endingShare:   0,
			namespaceID:   appconsts.TxNamespaceID,
			expectErr:     true,
		},
		{
			name:          "ending share higher than number of shares available in square size of 32",
			startingShare: 0,
			endingShare:   4097,
			namespaceID:   appconsts.TxNamespaceID,
			expectErr:     true,
		},
		{
			name:          "1 transaction share",
			startingShare: 0,
			endingShare:   0,
			namespaceID:   appconsts.TxNamespaceID,
			expectErr:     false,
		},
		{
			name:          "10 transaction shares",
			startingShare: 0,
			endingShare:   9,
			namespaceID:   appconsts.TxNamespaceID,
			expectErr:     false,
		},
		{
			name:          "50 transaction shares",
			startingShare: 0,
			endingShare:   49,
			namespaceID:   appconsts.TxNamespaceID,
			expectErr:     false,
		},
		{
			name:          "shares from different namespaces",
			startingShare: 48,
			endingShare:   54,
			namespaceID:   appconsts.TxNamespaceID,
			expectErr:     true,
		},
		{
			name:          "20 custom namespace shares",
			startingShare: 106,
			endingShare:   125,
			namespaceID:   []byte{0, 0, 0, 0, 0, 1, 0, 0},
			expectErr:     false,
		},
		{
			name:          "40 custom namespace shares",
			startingShare: 355,
			endingShare:   394,
			namespaceID:   []byte{0, 0, 1, 0, 0, 0, 0, 0},
			expectErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualNID, err := ParseNamespaceID(rawShares, tt.startingShare, tt.endingShare)
			if tt.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.namespaceID, actualNID)
			proof, err := GenerateSharesInclusionProof(
				rawShares,
				blockData.SquareSize,
				tt.namespaceID,
				uint64(tt.startingShare),
				uint64(tt.endingShare),
			)
			require.NoError(t, err)
			assert.NoError(t, proof.Validate(dataRoot))
		})
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
			start, end, err := TxSharePosition(tt.txs, uint64(i))
			require.NoError(t, err)
			positions[i] = startEndPoints{start: start, end: end}
		}

		splitShares := shares.SplitTxs(tt.txs)

		for i, pos := range positions {
			rawTx := []byte(tt.txs[i])
			rawTxDataForRange, err := stripCompactShares(splitShares[pos.start : pos.end+1])
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
