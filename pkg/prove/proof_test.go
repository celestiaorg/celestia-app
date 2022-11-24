package prove

import (
	"math/rand"
	"sort"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/da"
	"github.com/celestiaorg/celestia-app/testutil/namespace"
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

func TestShareInclusion(t *testing.T) {
	blobs := append(
		generateBlobsWithNamespace(
			100,
			500,
			[]byte{0, 0, 0, 0, 0, 1, 0, 0},
		),
		append(
			generateBlobsWithNamespace(
				50,
				500,
				[]byte{0, 0, 0, 1, 0, 0, 0, 0},
			),
			generateBlobsWithNamespace(
				50,
				500,
				[]byte{0, 0, 1, 0, 0, 0, 0, 0},
			)...,
		)...,
	)
	sort.Sort(blobs)
	blockData := types.Data{
		Txs:        generateRandomTxs(50, 500),
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
		shouldPass    bool
	}
	tests := []test{
		{
			name:          "negative starting share",
			startingShare: -1,
			endingShare:   99,
			namespaceID:   appconsts.TxNamespaceID,
			shouldPass:    false,
		},
		{
			name:          "negative ending share",
			startingShare: 0,
			endingShare:   -99,
			namespaceID:   appconsts.TxNamespaceID,
			shouldPass:    false,
		},
		{
			name:          "ending share bigger than starting share",
			startingShare: 1,
			endingShare:   0,
			namespaceID:   appconsts.TxNamespaceID,
			shouldPass:    false,
		},
		{
			name:          "ending share bigger than block shares number",
			startingShare: 0,
			endingShare:   4097,
			namespaceID:   appconsts.TxNamespaceID,
			shouldPass:    false,
		},
		{
			name:          "1 transaction share",
			startingShare: 0,
			endingShare:   0,
			namespaceID:   appconsts.TxNamespaceID,
			shouldPass:    true,
		},
		{
			name:          "10 transaction shares",
			startingShare: 0,
			endingShare:   9,
			namespaceID:   appconsts.TxNamespaceID,
			shouldPass:    true,
		},
		{
			name:          "50 transaction shares",
			startingShare: 0,
			endingShare:   49,
			namespaceID:   appconsts.TxNamespaceID,
			shouldPass:    true,
		},
		{
			name:          "shares from different namespaces",
			startingShare: 48,
			endingShare:   54,
			namespaceID:   appconsts.TxNamespaceID,
			shouldPass:    false,
		},
		{
			name:          "20 custom namespace shares",
			startingShare: 106,
			endingShare:   125,
			namespaceID:   []byte{0, 0, 0, 0, 0, 1, 0, 0},
			shouldPass:    true,
		},
		{
			name:          "40 custom namespace shares",
			startingShare: 201,
			endingShare:   250,
			namespaceID:   []byte{0, 0, 1, 0, 0, 0, 0, 0},
			shouldPass:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualNID, err := parseNamespaceID(rawShares, tt.startingShare, tt.endingShare)
			if !tt.shouldPass {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.namespaceID, actualNID)
			proof, err := SharesInclusion(
				rawShares,
				blockData.SquareSize,
				tt.namespaceID,
				uint64(tt.startingShare),
				uint64(tt.endingShare),
			)
			require.NoError(t, err)
			assert.NoError(t, proof.Validate())
			assert.Equal(t, dataRoot, proof.RowsProof.Root)
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

// TODO: Uncomment/fix this test after we've adjusted tx inclusion proofs to
// work using non-interactive defaults
// func Test_genRowShares(t *testing.T) {
//  squareSize := uint64(16)
//  typicalBlockData := types.Data{
//      Txs:                generateRandomlySizedTxs(10, 200),
//      Blobs:           generateRandomlySizedMessages(20, 1000),
//      SquareSize: squareSize,
//  }

// 	// note: we should be able to compute row shares from raw data
// 	// this quickly tests this by computing the row shares before
// 	// computing the shares in the normal way.
// 	rowShares, err := genRowShares(
// 		appconsts.DefaultCodec(),
// 		typicalBlockData,
// 		0,
// 		squareSize,
// 	)
// 	require.NoError(t, err)

// 	rawShares, err := shares.Split(typicalBlockData, false)
// 	require.NoError(t, err)

// 	eds, err := da.ExtendShares(squareSize, rawShares)
// 	require.NoError(t, err)

// 	for i := uint64(0); i < squareSize; i++ {
// 		row := eds.Row(uint(i))
// 		assert.Equal(t, row, rowShares[i], fmt.Sprintf("row %d", i))
// 		// also test fetching individual rows
// 		secondSet, err := genRowShares(appconsts.DefaultCodec(), typicalBlockData, i, i)
// 		require.NoError(t, err)
// 		assert.Equal(t, row, secondSet[0], fmt.Sprintf("row %d", i))
// 	}
// }

// func Test_genOrigRowShares(t *testing.T) {
// 	txCount := 100
// 	squareSize := uint64(16)
// 	typicalBlockData := types.Data{
// 		Txs:                generateRandomlySizedTxs(txCount, 200),
// 		Blobs:           generateRandomlySizedMessages(10, 1500),
// 		SquareSize: squareSize,
// 	}

// 	rawShares, err := shares.Split(typicalBlockData, false)
// 	require.NoError(t, err)

// 	genShares := genOrigRowShares(typicalBlockData, 0, 15)

// 	require.Equal(t, len(rawShares), len(genShares))
// 	assert.Equal(t, rawShares, genShares)
// }

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

// generateRandomlySizedBlobs generates randomly sized blobs with random
// namespace ID.
func generateRandomlySizedBlobs(count, maxBlobSize int) types.BlobsByNamespace {
	blobs := make([]types.Blob, count)
	for i := 0; i < count; i++ {
		blobs[i] = generateBlob(rand.Intn(maxBlobSize), namespace.RandomMessageNamespace())
	}

	// this is just to let us use assert.Equal
	if count == 0 {
		blobs = nil
	}

	sort.Sort(types.BlobsByNamespace(blobs))
	return blobs
}

// generateBlobsWithNamespace generates randomly sized blobs with
// namespace ID `nID`.
func generateBlobsWithNamespace(count, msgSize int, nID nmtnamespace.ID) types.BlobsByNamespace {
	blobs := make([]types.Blob, count)
	for i := 0; i < count; i++ {
		blobs[i] = generateBlob(msgSize, nID)
	}

	// this is just to let us use assert.Equal
	if count == 0 {
		blobs = nil
	}

	return blobs
}

func generateBlob(size int, nID nmtnamespace.ID) types.Blob {
	blob := types.Blob{
		NamespaceID: nID,
		Data:        tmrand.Bytes(size),
	}
	return blob
}
