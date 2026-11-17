package proof_test

import (
	"runtime"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/proof"
	"github.com/celestiaorg/celestia-app/v11/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v11/test/util/random"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v11/test/util/testnode"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	square "github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/stretchr/testify/require"
)

// TestNewTxInclusionProofRejectsTxsAfterFibreTx checks that a normal tx or a
// blob tx after a pay-for-fibre tx is rejected, not regrouped.
func TestNewTxInclusionProofRejectsTxsAfterFibreTx(t *testing.T) {
	signer, err := testnode.NewOfflineSigner()
	require.NoError(t, err)
	blobTx := blobfactory.RandBlobTxs(signer, random.New(), 1, 1, 500).ToSliceOfBytes()[0]
	normalTx := testfactory.GenerateRandomTxs(1, 500).ToSliceOfBytes()[0]
	txConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...).TxConfig
	fibreTx := blobfactory.UnsignedPayForFibreTx(t, txConfig)

	for name, txs := range map[string][][]byte{
		"normal after fibre": {fibreTx, normalTx},
		"blob after fibre":   {fibreTx, blobTx},
	} {
		t.Run(name, func(t *testing.T) {
			for i := range txs {
				_, err := proof.NewTxInclusionProof(txs, uint64(i), appconsts.Version)
				require.Error(t, err, "tx index %d", i)
			}
		})
	}
}

// TestNewTxInclusionProofBuildsSquareOnce checks that NewTxInclusionProof
// builds the data square only once. The baseline is one square build plus
// the share proof for the same range.
func TestNewTxInclusionProofBuildsSquareOnce(t *testing.T) {
	txs := make([][]byte, 200_000)
	for i := range txs {
		txs[i] = []byte{byte(i)}
	}
	const txIndex = 0

	// The first proof in a process sets up erasure coding tables. Do that
	// before measuring.
	_, err := proof.NewTxInclusionProof(txs, txIndex, appconsts.Version)
	require.NoError(t, err)

	baseline := totalAlloc(func() {
		classified, err := fibretypes.ClassifyTxs(txs)
		require.NoError(t, err)
		dataSquare, err := square.Construct(classified, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
		require.NoError(t, err)
		shareRange, err := square.TxShareRange(classified, txIndex, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
		require.NoError(t, err)
		_, err = proof.NewShareInclusionProof(dataSquare, share.TxNamespace, shareRange)
		require.NoError(t, err)
	})
	// TxShareRange builds the square a second time. Remove that from the
	// baseline so the baseline is one build.
	classified, err := fibretypes.ClassifyTxs(txs)
	require.NoError(t, err)
	secondBuild := totalAlloc(func() {
		_, err := square.TxShareRange(classified, txIndex, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
		require.NoError(t, err)
	})
	baseline -= secondBuild

	got := totalAlloc(func() {
		_, err := proof.NewTxInclusionProof(txs, txIndex, appconsts.Version)
		require.NoError(t, err)
	})

	// Allow 10% of slack for small costs. A second
	// square build adds secondBuild, which is far larger than that.
	require.Less(t, got, baseline+baseline/10,
		"NewTxInclusionProof allocated %d bytes; one square build plus the proof is %d bytes; one extra build is %d bytes",
		got, baseline, secondBuild)
}

func totalAlloc(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}
