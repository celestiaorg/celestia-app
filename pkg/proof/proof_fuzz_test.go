package proof

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/da"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/stretchr/testify/require"
)

// maxFuzzProofSize bounds the proof bytes a fuzz iteration decodes. Real
// proofs cover at most one row span of a max-size square and are far smaller.
const maxFuzzProofSize = 256 << 10

// shareProofSeed is a valid share proof with the data root it proves against.
type shareProofSeed struct {
	raw  []byte
	root []byte
}

// shareProofSeeds returns the single-share proof from the unit tests and a
// multi-row proof built from a square of random txs.
func shareProofSeeds(tb testing.TB) []shareProofSeed {
	tb.Helper()
	single := validShareProof()
	singleRaw, err := single.Marshal()
	require.NoError(tb, err)
	seeds := []shareProofSeed{{raw: singleRaw, root: root}}

	txs := testfactory.GenerateRandomTxs(40, 500)
	classified, err := fibretypes.ClassifyTxs(txs.ToSliceOfBytes())
	require.NoError(tb, err)
	dataSquare, err := square.Construct(classified, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
	require.NoError(tb, err)
	eds, err := da.ExtendShares(share.ToBytes(dataSquare))
	require.NoError(tb, err)
	dah, err := da.NewDataAvailabilityHeader(eds)
	require.NoError(tb, err)
	squareSize, err := dataSquare.Size()
	require.NoError(tb, err)
	require.Greater(tb, squareSize, 1, "the multi-row seed needs a square with more than one row")
	// Shares from the end of the first row to the start of the second.
	multi, err := NewShareInclusionProofFromEDS(eds, share.TxNamespace, share.NewRange(squareSize-1, squareSize+1))
	require.NoError(tb, err)
	multiRaw, err := multi.Marshal()
	require.NoError(tb, err)
	return append(seeds, shareProofSeed{raw: multiRaw, root: dah.Hash()})
}

// checkShareProof decodes raw as a share proof and validates it against root
// and against a root it cannot match. Neither may panic.
func checkShareProof(t *testing.T, raw, root []byte) {
	t.Helper()
	var sp ShareProof
	if err := sp.Unmarshal(raw); err != nil {
		return
	}
	// A proof without a row proof dereferences nil in Validate and in
	// VerifyProof; tracked in PROTOCO-3433 (item 5). VerifyProof is only
	// reached through Validate, which checks the proof and row root counts
	// agree first, so it is not called on its own here. RowProof.Validate is
	// still exercised below for every input.
	if sp.RowProof != nil {
		_ = sp.Validate(root)
		require.Error(t, sp.Validate(incorrectRoot), "share proof validated against a root it cannot match")
	}
	rp := sp.GetRowProof()
	if rp == nil {
		rp = &RowProof{}
	}
	_ = rp.Validate(root)
	require.Error(t, rp.Validate(incorrectRoot), "row proof validated against a root it cannot match")
}

func TestShareProofSeeds(t *testing.T) {
	for i, seed := range shareProofSeeds(t) {
		var sp ShareProof
		require.NoError(t, sp.Unmarshal(seed.raw))
		require.NoError(t, sp.Validate(seed.root), "seed %d must validate", i)
		checkShareProof(t, seed.raw, seed.root)
	}
}

// FuzzShareProofValidate feeds mutated share proof bytes, as a client or
// light node receives them, through share proof and row proof validation.
func FuzzShareProofValidate(f *testing.F) {
	seeds := shareProofSeeds(f)
	for _, seed := range seeds {
		f.Add(seed.raw, seed.root)
	}
	f.Add([]byte{}, incorrectRoot)
	f.Add(bytes.Repeat([]byte{0xff}, 8), seeds[0].root)
	f.Fuzz(func(t *testing.T, raw, root []byte) {
		if len(raw) > maxFuzzProofSize {
			return
		}
		checkShareProof(t, raw, root)
	})
}
