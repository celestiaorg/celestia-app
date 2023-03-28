package proof

import (
	"bytes"
	"sort"
	"testing"

	"github.com/celestiaorg/celestia-app/testutil/testfactory"

	"github.com/celestiaorg/celestia-app/pkg/da"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/types"
)

func TestNewTxInclusionProof(t *testing.T) {
	blockData := types.Data{
		Txs:        testfactory.GenerateRandomTxs(50, 500),
		Blobs:      []types.Blob{},
		SquareSize: appconsts.DefaultMaxSquareSize,
	}

	type test struct {
		name      string
		data      types.Data
		txIndex   uint64
		expectErr bool
	}
	tests := []test{
		{
			name:      "empty data returns error",
			data:      types.Data{},
			txIndex:   0,
			expectErr: true,
		},
		{
			name:      "txIndex 0 of block data",
			data:      blockData,
			txIndex:   0,
			expectErr: false,
		},
		{
			name:      "txIndex 49 of block data",
			data:      blockData,
			txIndex:   49,
			expectErr: false,
		},
		{
			name:      "txIndex 50 of block data returns error because only 50 txs",
			data:      blockData,
			txIndex:   50,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proof, err := NewTxInclusionProof(
				tt.data,
				tt.txIndex,
			)
			if tt.expectErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.True(t, proof.VerifyProof())
		})
	}
}

func TestNewShareInclusionProof(t *testing.T) {
	ns1 := appns.MustNewV0(bytes.Repeat([]byte{1}, appns.NamespaceVersionZeroIDSize))
	namespaceTwo := appns.MustNewV0(bytes.Repeat([]byte{2}, appns.NamespaceVersionZeroIDSize))
	namespaceThree := appns.MustNewV0(bytes.Repeat([]byte{3}, appns.NamespaceVersionZeroIDSize))

	blobs := append(
		testfactory.GenerateBlobsWithNamespace(
			100,
			500,
			ns1,
		),
		append(
			testfactory.GenerateBlobsWithNamespace(
				50,
				500,
				namespaceTwo,
			),
			testfactory.GenerateBlobsWithNamespace(
				50,
				500,
				namespaceThree,
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
		namespaceID   appns.Namespace
		expectErr     bool
	}
	tests := []test{
		{
			name:          "negative starting share",
			startingShare: -1,
			endingShare:   99,
			namespaceID:   appns.TxNamespace,
			expectErr:     true,
		},
		{
			name:          "negative ending share",
			startingShare: 0,
			endingShare:   -99,
			namespaceID:   appns.TxNamespace,
			expectErr:     true,
		},
		{
			name:          "ending share lower than starting share",
			startingShare: 1,
			endingShare:   0,
			namespaceID:   appns.TxNamespace,
			expectErr:     true,
		},
		{
			name:          "ending share higher than number of shares available in square size of 32",
			startingShare: 0,
			endingShare:   4097,
			namespaceID:   appns.TxNamespace,
			expectErr:     true,
		},
		{
			name:          "1 transaction share",
			startingShare: 0,
			endingShare:   0,
			namespaceID:   appns.TxNamespace,
			expectErr:     false,
		},
		{
			name:          "10 transaction shares",
			startingShare: 0,
			endingShare:   9,
			namespaceID:   appns.TxNamespace,
			expectErr:     false,
		},
		{
			name:          "50 transaction shares",
			startingShare: 0,
			endingShare:   49,
			namespaceID:   appns.TxNamespace,
			expectErr:     false,
		},
		{
			name:          "shares from different namespaces",
			startingShare: 48,
			endingShare:   54,
			namespaceID:   appns.TxNamespace,
			expectErr:     true,
		},
		{
			name:          "20 custom namespace shares",
			startingShare: 106,
			endingShare:   125,
			namespaceID:   ns1,
			expectErr:     false,
		},
		{
			name:          "40 custom namespace shares",
			startingShare: 355,
			endingShare:   394,
			namespaceID:   namespaceThree,
			expectErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualNID, err := ParseNamespace(rawShares, tt.startingShare, tt.endingShare)
			if tt.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.namespaceID, actualNID)
			proof, err := NewShareInclusionProof(
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

func TestTxShareRange(t *testing.T) {
	type test struct {
		name      string
		data      types.Data
		txIndex   uint64
		wantStart uint64
		wantEnd   uint64
		wantErr   bool
	}

	txOne := types.Tx{0x1}
	txTwo := types.Tx(bytes.Repeat([]byte{2}, 600))
	txThree := types.Tx(bytes.Repeat([]byte{3}, 1000))

	testCases := []test{
		{
			name: "expect err when txIndex is greater than the number of txs",
			data: types.Data{
				Txs:        []types.Tx{txOne},
				Blobs:      []types.Blob{},
				SquareSize: appconsts.DefaultMinSquareSize,
			},
			txIndex:   2,
			wantStart: 0,
			wantEnd:   0,
		},
		{
			name: "txOne occupies shares 0 to 0",
			data: types.Data{
				Txs:        []types.Tx{txOne},
				Blobs:      []types.Blob{},
				SquareSize: appconsts.DefaultMinSquareSize,
			},
			txIndex:   0,
			wantStart: 0,
			wantEnd:   0,
		},
		{
			name: "txTwo occupies shares 0 to 1",
			data: types.Data{
				Txs:        []types.Tx{txTwo},
				Blobs:      []types.Blob{},
				SquareSize: appconsts.DefaultMaxSquareSize,
			},
			txIndex:   0,
			wantStart: 0,
			wantEnd:   1,
		},
		{
			name: "txThree occupies shares 0 to 2",
			data: types.Data{
				Txs:        []types.Tx{txThree},
				Blobs:      []types.Blob{},
				SquareSize: appconsts.DefaultMaxSquareSize,
			},
			txIndex:   0,
			wantStart: 0,
			wantEnd:   2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			start, end, err := TxShareRange(tc.data, tc.txIndex)
			if tc.wantErr {
				assert.Error(t, err)
			}
			assert.Equal(t, tc.wantStart, start)
			assert.Equal(t, tc.wantEnd, end)
		})
	}
}
