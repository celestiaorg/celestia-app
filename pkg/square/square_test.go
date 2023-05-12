package square_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/da"
	"github.com/celestiaorg/celestia-app/pkg/inclusion"
	ns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/pkg/square"
	"github.com/celestiaorg/celestia-app/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	blob "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/celestiaorg/rsmt2d"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// FuzzSquareConstruction uses fuzzing to test the following:
// - That neither `Construct` or `Reconstruct` panics
// - That `Construct` never errors
// - That `Reconstruct` never errors from the input of `Construct`'s output
// - That both `Construct` and `Reconstruct` return the same square
// - That the square can be extended and a data availability header can be generated
func FuzzSquareBuildAndConstruction(f *testing.F) {
	var (
		normalTxCount uint = 123
		pfbCount      uint = 217
		pfbSize       uint = 8
	)
	f.Add(normalTxCount, pfbCount, pfbSize)
	f.Fuzz(func(t *testing.T, normalTxCount uint, pfbCount uint, pfbSize uint) {
		// ignore invalid values
		if pfbCount > 0 && pfbSize == 0 {
			t.Skip()
		}
		txs := generateMixedTxs(int(normalTxCount), int(pfbCount), int(pfbSize))
		s, newTxs, err := square.Build(txs, appconsts.DefaultMaxSquareSize)
		require.NoError(t, err)
		s2, err := square.Construct(newTxs, appconsts.DefaultMaxSquareSize)
		require.NoError(t, err)
		require.True(t, s.Equals(s2))

		eds, err := da.ExtendShares(shares.ToBytes(s))
		require.NoError(t, err)
		_ = da.NewDataAvailabilityHeader(eds)
	})
}

func TestSquareConstruction(t *testing.T) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	sendTxs := blobfactory.GenerateManyRawSendTxs(encCfg.TxConfig, 10)
	pfbTxs := blobfactory.RandBlobTxs(encCfg.TxConfig.TxEncoder(), 10, 100)
	t.Run("normal transactions after PFB trasactions", func(t *testing.T) {
		txs := append(sendTxs[:5], append(pfbTxs, sendTxs[5:]...)...)
		_, err := square.Construct(coretypes.Txs(txs).ToSliceOfBytes(), appconsts.DefaultMaxSquareSize)
		require.Error(t, err)
	})
	t.Run("not enough space to append transactions", func(t *testing.T) {
		_, err := square.Construct(coretypes.Txs(sendTxs).ToSliceOfBytes(), 2)
		require.Error(t, err)
		_, err = square.Construct(coretypes.Txs(pfbTxs).ToSliceOfBytes(), 2)
		require.Error(t, err)
	})
}

// TestSquareBlobPositions ensures that the share commitment rules which dictate the padding
// between blobs is followed as well as the ordering of blobs by namespace.
func TestSquareBlobPostions(t *testing.T) {
	ns1 := ns.MustNewV0(bytes.Repeat([]byte{1}, ns.NamespaceVersionZeroIDSize))
	ns2 := ns.MustNewV0(bytes.Repeat([]byte{2}, ns.NamespaceVersionZeroIDSize))
	ns3 := ns.MustNewV0(bytes.Repeat([]byte{3}, ns.NamespaceVersionZeroIDSize))

	type test struct {
		squareSize      int
		blobTxs         [][]byte
		expectedIndexes [][]uint32
	}
	tests := []test{
		{
			squareSize: 4,
			blobTxs: generateBlobTxsWithNamespaces(
				t,
				[]ns.Namespace{ns1},
				[][]int{{1}},
			),
			expectedIndexes: [][]uint32{{1}},
		},
		{
			squareSize: 4,
			blobTxs: generateBlobTxsWithNamespaces(
				t,
				[]ns.Namespace{ns1, ns1},
				blobfactory.Repeat([]int{100}, 2),
			),
			expectedIndexes: [][]uint32{{2}, {3}},
		},
		{
			squareSize: 4,
			blobTxs: generateBlobTxsWithNamespaces(
				t,
				[]ns.Namespace{ns1, ns1, ns1, ns1, ns1, ns1, ns1, ns1, ns1},
				blobfactory.Repeat([]int{100}, 9),
			),
			expectedIndexes: [][]uint32{{7}, {8}, {9}, {10}, {11}, {12}, {13}, {14}, {15}},
		},
		{
			squareSize: 4,
			blobTxs: generateBlobTxsWithNamespaces(
				t,
				[]ns.Namespace{ns1, ns1, ns1},
				[][]int{{10000}, {10000}, {1000000}},
			),
			expectedIndexes: [][]uint32{},
		},
		{
			squareSize: 64,
			blobTxs: generateBlobTxsWithNamespaces(
				t,
				[]ns.Namespace{ns1, ns1, ns1},
				[][]int{{1000}, {10000}, {10000}},
			),
			expectedIndexes: [][]uint32{{3}, {6}, {27}},
		},
		{
			squareSize: 32,
			blobTxs: generateBlobTxsWithNamespaces(
				t,
				[]ns.Namespace{ns2, ns1, ns1},
				[][]int{{100}, {100}, {100}},
			),
			expectedIndexes: [][]uint32{{5}, {3}, {4}},
		},
		{
			squareSize: 16,
			blobTxs: generateBlobTxsWithNamespaces(
				t,
				[]ns.Namespace{ns1, ns2, ns1},
				[][]int{{100}, {900}, {900}}, // 1, 2, 2 shares respectively
			),
			expectedIndexes: [][]uint32{{3}, {6}, {4}},
		},
		{
			squareSize: 4,
			blobTxs: generateBlobTxsWithNamespaces(
				t,
				[]ns.Namespace{ns1, ns3, ns3, ns2},
				[][]int{{100}, {1000, 1000}, {420}},
			),
			expectedIndexes: [][]uint32{{3}, {5, 8}, {4}},
		},
		{
			// no blob txs should make it in the square
			squareSize: 1,
			blobTxs: generateBlobTxsWithNamespaces(
				t,
				[]ns.Namespace{ns1, ns2, ns3},
				[][]int{{1000}, {1000}, {1000}},
			),
			expectedIndexes: [][]uint32{},
		},
		{
			// only two blob txs should make it in the square (after reordering)
			squareSize: 4,
			blobTxs: generateBlobTxsWithNamespaces(
				t,
				[]ns.Namespace{ns3, ns2, ns1},
				[][]int{{2000}, {2000}, {5000}},
			),
			expectedIndexes: [][]uint32{{7}, {2}},
		},
		{
			squareSize: 4,
			blobTxs: generateBlobTxsWithNamespaces(
				t,
				[]ns.Namespace{ns3, ns3, ns2, ns1},
				[][]int{{1800, 1000}, {22000}, {1800}},
			),
			// should be ns1 and {ns3, ns3} as ns2 is too large
			expectedIndexes: [][]uint32{{6, 10}, {2}},
		},
		{
			squareSize: 4,
			blobTxs: generateBlobTxsWithNamespaces(
				t,
				[]ns.Namespace{ns1, ns3, ns3, ns1, ns2, ns2},
				[][]int{{100}, {1400, 900, 200, 200}, {420}},
			),
			expectedIndexes: [][]uint32{{3}, {7, 10, 4, 5}, {6}},
		},
		{
			squareSize: 4,
			blobTxs: generateBlobTxsWithNamespaces(
				t,
				[]ns.Namespace{ns1, ns3, ns3, ns1, ns2, ns2},
				[][]int{{100}, {900, 1400, 200, 200}, {420}},
			),
			expectedIndexes: [][]uint32{{3}, {7, 9, 4, 5}, {6}},
		},
		{
			squareSize: 16,
			blobTxs: generateBlobTxsWithNamespaces(
				t,
				[]ns.Namespace{ns1, ns1},
				[][]int{{100}, {shares.AvailableBytesFromSparseShares(appconsts.SubtreeRootThreshold)}},
			),
			// There should be one share padding between the two blobs
			expectedIndexes: [][]uint32{{2}, {3}},
		},
		{
			squareSize: 16,
			blobTxs: generateBlobTxsWithNamespaces(
				t,
				[]ns.Namespace{ns1, ns1},
				[][]int{{100}, {shares.AvailableBytesFromSparseShares(appconsts.SubtreeRootThreshold) + 1}},
			),
			// There should be one share padding between the two blobs
			expectedIndexes: [][]uint32{{2}, {4}},
		},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("case%d", i), func(t *testing.T) {
			square, _, err := square.Build(tt.blobTxs, tt.squareSize)
			require.NoError(t, err)
			txs, err := shares.ParseTxs(square)
			require.NoError(t, err)
			for j, tx := range txs {
				wrappedPFB, isWrappedPFB := coretypes.UnmarshalIndexWrapper(tx)
				require.True(t, isWrappedPFB)
				require.Equal(t, tt.expectedIndexes[j], wrappedPFB.ShareIndexes, j)
			}
		})
	}
}

func TestSquareTxShareRange(t *testing.T) {
	type test struct {
		name      string
		txs       [][]byte
		index     int
		wantStart int
		wantEnd   int
		expectErr bool
	}

	txOne := types.Tx{0x1}
	txTwo := types.Tx(bytes.Repeat([]byte{2}, 600))
	txThree := types.Tx(bytes.Repeat([]byte{3}, 1000))

	testCases := []test{
		{
			name:      "txOne occupies shares 0 to 0",
			txs:       [][]byte{txOne},
			index:     0,
			wantStart: 0,
			wantEnd:   1,
			expectErr: false,
		},
		{
			name:      "txTwo occupies shares 0 to 1",
			txs:       [][]byte{txTwo},
			index:     0,
			wantStart: 0,
			wantEnd:   2,
			expectErr: false,
		},
		{
			name:      "txThree occupies shares 0 to 2",
			txs:       [][]byte{txThree},
			index:     0,
			wantStart: 0,
			wantEnd:   3,
			expectErr: false,
		},
		{
			name:      "txThree occupies shares 1 to 3",
			txs:       [][]byte{txOne, txTwo, txThree},
			index:     2,
			wantStart: 1,
			wantEnd:   4,
			expectErr: false,
		},
		{
			name:      "invalid index",
			txs:       [][]byte{txOne, txTwo, txThree},
			index:     3,
			wantStart: 0,
			wantEnd:   0,
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			shareRange, err := square.TxShareRange(tc.txs, tc.index)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.wantStart, shareRange.Start)
			require.Equal(t, tc.wantEnd, shareRange.End)
		})
	}
}

// generateBlobTxsWithNamespaces will generate len(namespaces) BlobTxs with
// len(blobSizes[i]) number of blobs per BlobTx. Note: not suitable for using in
// prepare or process proposal, as the signatures will be invalid since this
// does not query for relevant account numbers or sequences.
func generateBlobTxsWithNamespaces(t *testing.T, namespaces []ns.Namespace, blobSizes [][]int) [][]byte {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	const acc = "signer"
	kr := testfactory.GenerateKeyring(acc)
	return blobfactory.ManyMultiBlobTx(
		t,
		encCfg.TxConfig.TxEncoder(),
		kr,
		"chainid",
		blobfactory.Repeat(acc, len(blobSizes)),
		blobfactory.Repeat(blobfactory.AccountInfo{}, len(blobSizes)),
		blobfactory.NestedBlobs(t, namespaces, blobSizes),
	)
}

func TestSquareBlobShareRange(t *testing.T) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	txs := blobfactory.RandBlobTxsRandomlySized(encCfg.TxConfig.TxEncoder(), 10, 1000, 10).ToSliceOfBytes()

	builder, err := square.NewBuilder(appconsts.DefaultMaxSquareSize, txs...)
	require.NoError(t, err)

	dataSquare, err := builder.Export()
	require.NoError(t, err)

	for pfbIdx, tx := range txs {
		blobTx, isBlobTx := coretypes.UnmarshalBlobTx(tx)
		require.True(t, isBlobTx)
		for blobIdx := range blobTx.Blobs {
			shareRange, err := square.BlobShareRange(txs, pfbIdx, blobIdx)
			require.NoError(t, err)
			blobShares := dataSquare[shareRange.Start : shareRange.End+1]
			blobSharesBytes, err := rawData(blobShares)
			require.NoError(t, err)
			require.True(t, bytes.Contains(blobSharesBytes, blobTx.Blobs[blobIdx].Data))
		}
	}

	// error on out of bounds cases
	_, err = square.BlobShareRange(txs, -1, 0)
	require.Error(t, err)

	_, err = square.BlobShareRange(txs, 0, -1)
	require.Error(t, err)

	_, err = square.BlobShareRange(txs, 10, 0)
	require.Error(t, err)

	_, err = square.BlobShareRange(txs, 0, 10)
	require.Error(t, err)
}

func TestSquareShareCommitments(t *testing.T) {
	const numTxs = 10
	txs := generateOrderedTxs(numTxs, numTxs, 5)
	builder, err := square.NewBuilder(appconsts.DefaultMaxSquareSize, txs...)
	require.NoError(t, err)

	dataSquare, err := builder.Export()
	require.NoError(t, err)

	cacher := inclusion.NewSubtreeCacher(uint64(dataSquare.Size()))
	eds, err := rsmt2d.ComputeExtendedDataSquare(shares.ToBytes(dataSquare), appconsts.DefaultCodec(), cacher.Constructor)
	require.NoError(t, err)
	dah := da.NewDataAvailabilityHeader(eds)
	decoder := encoding.MakeConfig(app.ModuleEncodingRegisters...).TxConfig.TxDecoder()

	for pfbIndex := 0; pfbIndex < numTxs; pfbIndex++ {
		wpfb, err := builder.GetWrappedPFB(pfbIndex + numTxs)
		require.NoError(t, err)
		tx, err := decoder(wpfb.Tx)
		require.NoError(t, err)

		pfb, ok := tx.GetMsgs()[0].(*blob.MsgPayForBlobs)
		require.True(t, ok)

		for blobIndex, shareIndex := range wpfb.ShareIndexes {
			commitment, err := inclusion.GetCommitment(cacher, dah, int(shareIndex), shares.SparseSharesNeeded(pfb.BlobSizes[blobIndex]))
			require.NoError(t, err)
			require.Equal(t, pfb.ShareCommitments[blobIndex], commitment)
		}
	}
}
