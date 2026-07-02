package rsema1d

import (
	"bytes"
	"crypto/sha256"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/merkle"
	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/rlc"
	"github.com/stretchr/testify/require"
)

func TestVerifySharedRejectsRowSizeMismatch(t *testing.T) {
	cfg := &Config{K: 64, N: 64, WorkerCount: 1}
	const (
		small = field.LeopardChunkSize     // 64B  -> 32 coefficients
		large = 2 * field.LeopardChunkSize // 128B -> 64 coefficients
	)

	// A mixed-length row tree: leaf 0 is the small priming row, leaf 1 is double
	// the size. The remaining leaves are unused.
	smallRow := bytes.Repeat([]byte{0x01}, small)
	largeRow := bytes.Repeat([]byte{0x02}, large)
	leaves := make([][]byte, cfg.K+cfg.N)
	leaves[0] = smallRow
	leaves[1] = largeRow
	tree := merkle.NewTree(leaves, cfg.WorkerCount)
	rowRoot := tree.Root()

	// rlcOrig holds the expected RLC value for each of the K original rows. The
	// first Verify below only passes row 0, so only rlcOrig[0] is checked: set it
	// to row 0's actual RLC so that Verify succeeds and leave the rest zero.
	coeffs := rlc.DeriveCoefficients(rowRoot, cfg.K, cfg.N, small, cfg.WorkerCount)
	rlcOrig := make(rlc.Vector, cfg.K)
	rlcOrig[0] = rlc.Compute([][]byte{smallRow}, coeffs, cfg.WorkerCount)[0]

	// The commitment binds rowRoot to rlcOrig, so both batches pass the
	// commitment check.
	commitment := commitmentFor(cfg, rowRoot, rlcOrig)

	v, err := NewVerifier(cfg)
	require.NoError(t, err)

	// Prime the cache with the valid small-row batch.
	smallProof := rowProofFromTree(t, tree, 0, smallRow)
	require.NoError(t, v.Verify(commitment, []*RowProof{smallProof}, rlcOrig))

	// The oversized batch clears validateProofs and the commitment check, then
	// hits the stale cache. It must be rejected, not panic.
	largeProof := rowProofFromTree(t, tree, 1, largeRow)
	err = v.VerifyShared(commitment, []*RowProof{largeProof})
	require.ErrorContains(t, err, "does not match cached row size")

	// And the verifier stays usable for a correctly-sized batch.
	require.NoError(t, v.VerifyShared(commitment, []*RowProof{smallProof}))
}

// commitmentFor builds SHA256(rowRoot || rlcRoot), deriving rlcRoot from rlcOrig
// the same way the verifier does internally.
func commitmentFor(cfg *Config, rowRoot merkle.Root, rlcOrig rlc.Vector) Commitment {
	var leafScratch [field.GF128Size]byte
	rlcRoot := computeRLCRoot(rlcOrig, make([]byte, cfg.K*merkle.NodeSize), leafScratch[:])
	h := sha256.New()
	h.Write(rowRoot[:])
	h.Write(rlcRoot[:])
	var commitment Commitment
	h.Sum(commitment[:0])
	return commitment
}

// rowProofFromTree builds a RowProof for a leaf index from a merkle tree.
func rowProofFromTree(t *testing.T, tree *merkle.Tree, index int, row []byte) *RowProof {
	t.Helper()
	path, err := tree.Proof(index)
	require.NoError(t, err)
	return &RowProof{Index: index, Row: row, RowProof: path}
}
