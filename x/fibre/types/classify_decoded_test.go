package types_test

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	fibrekeeper "github.com/celestiaorg/celestia-app/v11/x/fibre/keeper"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	squarev4 "github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/share"
	squaretx "github.com/celestiaorg/go-square/v4/tx"
	"github.com/cosmos/btcutil/bech32"
	"github.com/stretchr/testify/require"
)

// decodeCaches builds the per-proposal decode caches exactly the way
// ProcessProposal's up-front loop does.
func decodeCaches(txs [][]byte) (blobTxs []*squaretx.BlobTx, decoded []*fibretypes.DecodedPayForFibre) {
	blobTxs = make([]*squaretx.BlobTx, len(txs))
	decoded = make([]*fibretypes.DecodedPayForFibre, len(txs))
	for idx, rawTx := range txs {
		if len(rawTx) > appconsts.MaxTxSize {
			continue
		}
		decodedBlob, isBlobTx, err := squaretx.UnmarshalBlobTx(rawTx)
		if isBlobTx {
			if err != nil {
				// ProcessProposal rejects here and never classifies.
				continue
			}
			blobTxs[idx] = decodedBlob
		}
		if blobTxs[idx] == nil {
			decoded[idx] = fibrekeeper.DecodePayForFibre(rawTx)
		}
	}
	return blobTxs, decoded
}

func blobTxBytes(t *testing.T, rng *rand.Rand, blobSizes ...int) []byte {
	t.Helper()
	blobs := make([]*share.Blob, len(blobSizes))
	for i, size := range blobSizes {
		data := make([]byte, size)
		_, _ = rng.Read(data)
		blob, err := share.NewBlob(testNamespace, data, share.ShareVersionZero, nil)
		require.NoError(t, err)
		blobs[i] = blob
	}
	inner := make([]byte, 200)
	_, _ = rng.Read(inner)
	raw, err := squaretx.MarshalBlobTx(inner, blobs...)
	require.NoError(t, err)
	return raw
}

// TestClassifyTxsDecodedMatchesClassifyTxs checks classifying from the decode
// caches builds the same square as classifying from raw bytes. A disagreement
// here is a chain split, so the corpus covers every classification branch.
func TestClassifyTxsDecodedMatchesClassifyTxs(t *testing.T) {
	signer, err := bech32.EncodeFromBase256("celestia", testSignerRaw)
	require.NoError(t, err)
	rng := rand.New(rand.NewSource(3))

	oneBlob := blobTxBytes(t, rng, 150)
	nested, err := squaretx.MarshalBlobTx(oneBlob, func() *share.Blob {
		blob, err := share.NewBlob(testNamespace, []byte("nested"), share.ShareVersionZero, nil)
		require.NoError(t, err)
		return blob
	}())
	require.NoError(t, err)

	// Non-PFF shapes carrying the MsgPayForFibre type URL: a blob whose payload
	// carries the string, and a BlobTx wrapping a real pay-for-fibre tx.
	typeURLInBlob, err := squaretx.MarshalBlobTx([]byte("inner"), func() *share.Blob {
		blob, err := share.NewBlob(testNamespace, []byte(fibretypes.MsgPayForFibreTypeURL), share.ShareVersionZero, nil)
		require.NoError(t, err)
		return blob
	}())
	require.NoError(t, err)

	wrappedFibre, err := squaretx.MarshalBlobTx(
		fibreTxBytes(t, signer, testNamespace.Bytes(), testCommitment),
		func() *share.Blob {
			blob, err := share.NewBlob(testNamespace, []byte("payload"), share.ShareVersionZero, nil)
			require.NoError(t, err)
			return blob
		}())
	require.NoError(t, err)

	corpus := []struct {
		name string
		raw  []byte
	}{
		{"normal tx", []byte("an ordinary transaction")},
		{"empty tx", nil},
		{"undecodable bytes", []byte{0x00}},
		{"fibre tx", fibreTxBytes(t, signer, testNamespace.Bytes(), testCommitment)},
		{"malformed fibre tx", fibreTxBytes(t, "not-a-bech32-address", testNamespace.Bytes(), testCommitment)},
		{"blob tx, one blob", oneBlob},
		{"blob tx, three blobs", blobTxBytes(t, rng, 100, 600, 1200)},
		{"blob tx, multi-share blob", blobTxBytes(t, rng, 2500)},
		{"non-canonical blob tx", append(append([]byte(nil), oneBlob...), 0x78, 0x01)},
		{"nested blob tx", nested},
		{"blob tx whose blob carries the fibre type URL", typeURLInBlob},
		{"blob tx wrapping a fibre tx", wrappedFibre},
	}

	for _, maxSquareSize := range []int{8, 64, 128} {
		for iteration := range 200 {
			txs := make([][]byte, 1+rng.Intn(6))
			names := make([]string, len(txs))
			for i := range txs {
				entry := corpus[rng.Intn(len(corpus))]
				txs[i], names[i] = entry.raw, entry.name
			}
			where := fmt.Sprintf("maxSquareSize=%d iteration=%d txs=%v", maxSquareSize, iteration, names)

			blobTxs, decoded := decodeCaches(txs)
			got, gotErr := fibretypes.ClassifyTxsDecoded(txs, blobTxs, decoded)
			want, wantErr := fibretypes.ClassifyTxs(txs)
			require.Equal(t, fmt.Sprint(wantErr), fmt.Sprint(gotErr), "classification disagreed: %s", where)
			if wantErr != nil {
				continue
			}
			require.Len(t, got, len(want))

			for i := range want {
				require.Equal(t, want[i].Bytes, got[i].Bytes, "bytes at %d: %s", i, where)
				require.Equal(t, want[i].FibreTx, got[i].FibreTx, "fibre tx at %d: %s", i, where)
				// The one intended difference: the decoded form carries the blob tx.
				require.Nil(t, want[i].BlobTx, "index %d: %s", i, where)
				require.Equal(t, blobTxs[i], got[i].BlobTx, "blob tx at %d: %s", i, where)
			}

			gotSquare, gotErr := squarev4.Construct(got, maxSquareSize, appconsts.SubtreeRootThreshold)
			wantSquare, wantErr := squarev4.Construct(want, maxSquareSize, appconsts.SubtreeRootThreshold)
			require.Equal(t, fmt.Sprint(wantErr), fmt.Sprint(gotErr), "construction disagreed: %s", where)
			if wantErr != nil {
				continue
			}
			require.Equal(t, wantSquare, gotSquare, "squares differ: %s", where)
		}
	}
}

// TestClassifyTxsDecodedRejectsMisalignedCaches covers the guard against
// reading the wrong cache entry for a transaction.
func TestClassifyTxsDecodedRejectsMisalignedCaches(t *testing.T) {
	txs := [][]byte{[]byte("one"), []byte("two")}

	_, err := fibretypes.ClassifyTxsDecoded(txs, make([]*squaretx.BlobTx, 1), nil)
	require.ErrorContains(t, err, "not index aligned")

	_, err = fibretypes.ClassifyTxsDecoded(txs, nil, make([]*fibretypes.DecodedPayForFibre, 3))
	require.ErrorContains(t, err, "not index aligned")

	// Both nil is the plain ClassifyTxs path.
	classified, err := fibretypes.ClassifyTxsDecoded(txs, nil, nil)
	require.NoError(t, err)
	require.Len(t, classified, 2)
}
