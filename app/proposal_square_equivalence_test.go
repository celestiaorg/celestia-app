package app

import (
	"math/rand"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	fibrekeeper "github.com/celestiaorg/celestia-app/v10/x/fibre/keeper"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	squarev4 "github.com/celestiaorg/go-square/v4"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	"github.com/stretchr/testify/require"
)

// TestClassifyAndConstructMatchGoSquare pins the hand-rolled decode-once
// classification and square construction in process_proposal.go against the
// go-square implementations they replace.
//
// ProcessProposal now classifies from decoded objects and builds the square
// with local copies of go-square's ordering rules, so a go-square bump that
// changes `Construct` or `ClassifyTxs` must fail here rather than silently
// diverge: the two sides decide what a proposal's data root is.
//
// Real PayForFibre and blob transactions are exercised end to end by the
// app/test proposal suites; this test covers the classification and ordering
// rules themselves over a corpus that reaches every branch of both.
//
// Only the order in which two different rejections are reported may differ -
// `Construct` validates the whole ordering before appending, the local copy
// interleaves - so the comparison is accept/reject plus square bytes, not
// error text.
func TestClassifyAndConstructMatchGoSquare(t *testing.T) {
	rng := rand.New(rand.NewSource(7))

	// a corpus of tx shapes: ordinary bytes, undecodable bytes, and anything
	// else the generators below produce. Blob txs and fibre txs are built by
	// the test helpers in this package where available; where they are not,
	// the random corpus still exercises the classification branches.
	corpus := make([][]byte, 0, 28) // 4 fixed shapes plus the 24 random ones below
	corpus = append(corpus,
		nil,
		[]byte{},
		[]byte{0x00},
		[]byte("not a tx at all"),
	)
	for range 24 {
		raw := make([]byte, 1+rng.Intn(64))
		_, err := rng.Read(raw)
		require.NoError(t, err)
		corpus = append(corpus, raw)
	}

	for _, maxSquareSize := range []int{4, 8, 64, 128} {
		for range 400 {
			n := 1 + rng.Intn(7)
			txs := make([][]byte, n)
			for i := range txs {
				txs[i] = corpus[rng.Intn(len(corpus))]
			}

			// the decode caches ProcessProposal builds for this list
			blobTxs := make([]*blobtx.BlobTx, len(txs))
			decoded := make([]*fibretypes.DecodedPayForFibre, len(txs))
			for i, raw := range txs {
				if decodedBlob, isBlobTx, err := blobtx.UnmarshalBlobTx(raw); isBlobTx && err == nil {
					blobTxs[i] = decodedBlob
					continue
				}
				decoded[i] = fibrekeeper.DecodePayForFibre(raw)
			}

			gotClassified, gotErr := classifyTxs(txs, blobTxs, decoded)
			wantClassified, wantErr := fibretypes.ClassifyTxs(txs)
			require.Equal(t, wantErr != nil, gotErr != nil,
				"classification disagreed on %d txs: got %v, want %v", n, gotErr, wantErr)
			if wantErr != nil {
				continue
			}
			require.Equal(t, len(wantClassified), len(gotClassified))

			gotSquare, gotErr := constructSquare(gotClassified, blobTxs, maxSquareSize, appconsts.SubtreeRootThreshold)
			wantSquare, wantErr := squarev4.Construct(wantClassified, maxSquareSize, appconsts.SubtreeRootThreshold)
			require.Equal(t, wantErr != nil, gotErr != nil,
				"construction disagreed on %d txs at maxSquareSize %d: got %v, want %v", n, maxSquareSize, gotErr, wantErr)
			if wantErr != nil {
				continue
			}
			require.Equal(t, wantSquare, gotSquare, "squares differ at maxSquareSize %d", maxSquareSize)
		}
	}
}
