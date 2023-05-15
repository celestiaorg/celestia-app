package square_test

import (
	"bytes"
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
		s, newTxs, err := square.Build(txs, appconsts.LatestVersion)
		require.NoError(t, err)
		s2, err := square.Construct(newTxs, appconsts.LatestVersion)
		require.NoError(t, err)
		require.True(t, s.Equals(s2))

		eds, err := da.ExtendShares(shares.ToBytes(s))
		require.NoError(t, err)
		_ = da.NewDataAvailabilityHeader(eds)
	})
}

func TestSquareConstruction(t *testing.T) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	sendTxs := blobfactory.GenerateManyRawSendTxs(encCfg.TxConfig, 25000)
	pfbTxs := blobfactory.RandBlobTxs(encCfg.TxConfig.TxEncoder(), 10000, 1024)
	t.Run("normal transactions after PFB trasactions", func(t *testing.T) {
		txs := append(sendTxs[:5], append(pfbTxs, sendTxs[5:]...)...)
		_, err := square.Construct(coretypes.Txs(txs).ToSliceOfBytes(), appconsts.LatestVersion)
		require.Error(t, err)
	})
	t.Run("not enough space to append transactions", func(t *testing.T) {
		_, err := square.Construct(coretypes.Txs(sendTxs).ToSliceOfBytes(), appconsts.LatestVersion)
		require.Error(t, err)
		_, err = square.Construct(coretypes.Txs(pfbTxs).ToSliceOfBytes(), appconsts.LatestVersion)
		require.Error(t, err)
	})
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
			shareRange, err := square.TxShareRange(tc.txs, tc.index, appconsts.LatestVersion)
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

	builder, err := square.NewBuilder(appconsts.DefaultMaxSquareSize, appconsts.DefaultSubtreeRootThreshold, txs...)
	require.NoError(t, err)

	dataSquare, err := builder.Export()
	require.NoError(t, err)

	for pfbIdx, tx := range txs {
		blobTx, isBlobTx := coretypes.UnmarshalBlobTx(tx)
		require.True(t, isBlobTx)
		for blobIdx := range blobTx.Blobs {
			shareRange, err := square.BlobShareRange(txs, pfbIdx, blobIdx, appconsts.LatestVersion)
			require.NoError(t, err)
			blobShares := dataSquare[shareRange.Start : shareRange.End+1]
			blobSharesBytes, err := rawData(blobShares)
			require.NoError(t, err)
			require.True(t, bytes.Contains(blobSharesBytes, blobTx.Blobs[blobIdx].Data))
		}
	}

	// error on out of bounds cases
	_, err = square.BlobShareRange(txs, -1, 0, appconsts.LatestVersion)
	require.Error(t, err)

	_, err = square.BlobShareRange(txs, 0, -1, appconsts.LatestVersion)
	require.Error(t, err)

	_, err = square.BlobShareRange(txs, 10, 0, appconsts.LatestVersion)
	require.Error(t, err)

	_, err = square.BlobShareRange(txs, 0, 10, appconsts.LatestVersion)
	require.Error(t, err)
}

func TestSquareShareCommitments(t *testing.T) {
	const numTxs = 10
	txs := generateOrderedTxs(numTxs, numTxs, 5)
	builder, err := square.NewBuilder(appconsts.DefaultMaxSquareSize, appconsts.DefaultSubtreeRootThreshold, txs...)
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
			commitment, err := inclusion.GetCommitment(cacher, dah, int(shareIndex), shares.SparseSharesNeeded(pfb.BlobSizes[blobIndex]), appconsts.DefaultSubtreeRootThreshold)
			require.NoError(t, err)
			require.Equal(t, pfb.ShareCommitments[blobIndex], commitment)
		}
	}
}
