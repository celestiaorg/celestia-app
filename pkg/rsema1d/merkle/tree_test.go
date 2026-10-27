package merkle_test

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d/merkle"
	cmtmerkle "github.com/cometbft/cometbft/crypto/merkle"
	"github.com/stretchr/testify/require"
)

// TestTree checks trees against CometBFT, which they must stay wire- and
// proof-compatible with: the root, every proof from [merkle.Tree.Proof] and
// [merkle.Tree.Proofs], and that [merkle.RootFromProof] and
// [merkle.RootFromProofs] recompute the root from those proofs.
func TestTree(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	const mixed = -1 // leaf sizes vary per leaf
	for _, n := range []int{1, 2, 4, 64, 1024} {
		for _, size := range []int{0, 1, 63, 64, 65, 4160, mixed} {
			leaves := make([][]byte, n)
			for i := range leaves {
				leafSize := size
				if size == mixed {
					leafSize = rng.IntN(200)
				}
				leaves[i] = randBytes(rng, leafSize)
			}
			wantRoot, wantProofs := cmtmerkle.ProofsFromByteSlices(leaves)

			for _, workers := range []int{1, 4} {
				t.Run(fmt.Sprintf("leaves=%d/size=%d/workers=%d", n, size, workers), func(t *testing.T) {
					tree := merkle.NewTree(nil, leaves, workers)
					root := tree.Root()
					require.Equal(t, wantRoot, root[:])

					inputs := make([]merkle.ProofInput, n)
					for i, leaf := range leaves {
						proof, err := tree.Proof(i)
						require.NoError(t, err)
						requireAunts(t, wantProofs[i].Aunts, proof)

						got, err := merkle.RootFromProof(leaf, i, proof)
						require.NoError(t, err)
						require.Equal(t, root, got)
						inputs[i] = merkle.ProofInput{Leaf: leaf, Index: i, Path: proof}
					}

					positions := make([]int, n)
					for i := range positions {
						positions[i] = n - 1 - i
					}
					require.NoError(t, tree.Proofs(positions, func(i int, proof [][]byte) {
						requireAunts(t, wantProofs[positions[i]].Aunts, proof)
					}))

					// odd- and even-length batches, starting at either parity
					for start := range min(n, 3) {
						got, err := merkle.RootFromProofs(inputs[start:], workers)
						require.NoError(t, err)
						require.Equal(t, root, got)
					}
				})
			}
		}
	}
}

func TestTreeInvalid(t *testing.T) {
	for _, n := range []int{0, 3, 5} {
		require.Panics(t, func() { merkle.NewTree(nil, makeTestLeaves(n), 1) }, "%d leaves", n)
	}
	require.Panics(t, func() {
		merkle.NewTree(make([]byte, merkle.TreeBufferSize(8)), makeTestLeaves(4), 1)
	}, "buffer sized for another leaf count")

	tree := merkle.NewTree(nil, makeTestLeaves(8), 1)
	for _, index := range []int{-1, 8, 100} {
		_, err := tree.Proof(index)
		require.Error(t, err, "Proof(%d)", index)
		require.Error(t, tree.Proofs([]int{0, index}, func(int, [][]byte) {}), "Proofs(%d)", index)
	}
}

// TestTreeCallerOwnedStorage checks that a caller-provided buffer becomes the
// tree's node storage, so rebuilding into it costs a few small allocations
// that do not grow with the leaf count.
func TestTreeCallerOwnedStorage(t *testing.T) {
	allocs := func(n int) float64 {
		leaves := makeTestLeaves(n)
		buf := make([]byte, merkle.TreeBufferSize(n))
		root := merkle.NewTree(buf, leaves, 1).Root()
		require.Equal(t, buf[:merkle.NodeSize], root[:], "tree must alias buf")
		return testing.AllocsPerRun(20, func() { merkle.NewTree(buf, leaves, 1) })
	}
	small, large := allocs(8), allocs(1024)
	require.Equal(t, small, large, "allocations grow with the leaf count")
	require.LessOrEqual(t, small, 4.0)
}

// requireAunts fails unless proof holds exactly the sibling hashes in want.
func requireAunts(t *testing.T, want, proof [][]byte) {
	t.Helper()
	require.Len(t, proof, len(want))
	for i := range want {
		require.Equal(t, want[i], proof[i], "sibling %d", i)
	}
}

func randBytes(rng *rand.Rand, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rng.Uint32())
	}
	return b
}

// makeTestLeaves builds n 32-byte leaves with a deterministic per-index pattern.
func makeTestLeaves(n int) [][]byte {
	leaves := make([][]byte, n)
	for i := range n {
		leaf := make([]byte, 32)
		for j := range leaf {
			leaf[j] = byte((i + j) % 256)
		}
		leaves[i] = leaf
	}
	return leaves
}
