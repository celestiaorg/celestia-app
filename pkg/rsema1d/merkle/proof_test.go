package merkle_test

import (
	"math/rand/v2"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d/merkle"
	"github.com/stretchr/testify/require"
)

// TestRootFromProofErrors checks that a single proof either errors or yields a
// different root when its leaf, index or siblings are tampered with.
func TestRootFromProofErrors(t *testing.T) {
	leaves := makeTestLeaves(16)
	tree := merkle.NewTree(nil, leaves, 1)
	proof, err := tree.Proof(5)
	require.NoError(t, err)

	withSibling := func(level int, sibling []byte) [][]byte {
		p := append([][]byte(nil), proof...)
		p[level] = sibling
		return p
	}
	flipped := append([]byte(nil), proof[2]...)
	flipped[0] ^= 1

	cases := []struct {
		name    string
		leaf    []byte
		index   int
		proof   [][]byte
		wantErr string // empty: no error, but the root must differ
	}{
		{"wrong_leaf", leaves[6], 5, proof, ""},
		{"wrong_index", leaves[5], 4, proof, ""},
		{"tampered_sibling", leaves[5], 5, withSibling(2, flipped), ""},
		{"short_path", leaves[5], 5, proof[:3], ""},
		{"short_sibling", leaves[5], 5, withSibling(1, proof[1][:31]), "proof sibling must be 32 bytes, got 31"},
		// padding past NodeSize must be rejected, not truncated to a valid prefix
		{"oversized_sibling", leaves[5], 5, withSibling(0, append(append([]byte(nil), proof[0]...), 0xAA)), "proof sibling must be 32 bytes, got 33"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, err := merkle.RootFromProof(tc.leaf, tc.index, tc.proof)
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotEqual(t, tree.Root(), root)
		})
	}
}

// TestRootFromProofsErrors checks that a batch with any bad proof fails, and
// that the sequential path reports the first bad input exactly.
func TestRootFromProofsErrors(t *testing.T) {
	rng := rand.New(rand.NewPCG(15, 16))
	const n = 16
	leaves := make([][]byte, n)
	for i := range leaves {
		leaves[i] = randBytes(rng, 256)
	}
	tree := merkle.NewTree(nil, leaves, 1)
	inputs := make([]merkle.ProofInput, n)
	for i := range leaves {
		proof, err := tree.Proof(i)
		require.NoError(t, err)
		inputs[i] = merkle.ProofInput{Leaf: leaves[i], Index: i, Path: proof}
	}

	// tamper deep-copies inputs, so cases never share mutated state
	tamper := func(mod func(in []merkle.ProofInput)) []merkle.ProofInput {
		cp := make([]merkle.ProofInput, n)
		for i, in := range inputs {
			cp[i] = in
			cp[i].Leaf = append([]byte(nil), in.Leaf...)
			cp[i].Path = make([][]byte, len(in.Path))
			for j := range in.Path {
				cp[i].Path[j] = append([]byte(nil), in.Path[j]...)
			}
		}
		mod(cp)
		return cp
	}
	cases := []struct {
		name string
		in   []merkle.ProofInput
		want string // exact error with one worker
	}{
		{"tampered_leaf_even", tamper(func(in []merkle.ProofInput) { in[4].Leaf[0]++ }), "input 4 (tree index 4): root mismatch"},
		{"tampered_leaf_odd", tamper(func(in []merkle.ProofInput) { in[5].Leaf[0]++ }), "input 5 (tree index 5): root mismatch"},
		// the first input sets the expected root, so the second one mismatches
		{"tampered_first", tamper(func(in []merkle.ProofInput) { in[0].Leaf[0]++ }), "input 1 (tree index 1): root mismatch"},
		{"tampered_last", tamper(func(in []merkle.ProofInput) { in[15].Leaf[0]++ }), "input 15 (tree index 15): root mismatch"},
		{"tampered_sibling", tamper(func(in []merkle.ProofInput) { in[6].Path[1][3]++ }), "input 6 (tree index 6): root mismatch"},
		{"wrong_index", tamper(func(in []merkle.ProofInput) { in[7].Index = 6 }), "input 7 (tree index 6): root mismatch"},
		{"short_sibling", tamper(func(in []merkle.ProofInput) { in[3].Path[0] = in[3].Path[0][:31] }), "input 3 (tree index 3): proof sibling must be 32 bytes, got 31"},
		{"short_sibling_first", tamper(func(in []merkle.ProofInput) { in[0].Path[2] = nil }), "input 0 (tree index 0): proof sibling must be 32 bytes, got 0"},
		{"short_path", tamper(func(in []merkle.ProofInput) { in[9].Path = in[9].Path[:2] }), "input 9 (tree index 9): root mismatch"},
		{"first_bad_wins", tamper(func(in []merkle.ProofInput) { in[10].Leaf[1]++; in[2].Path[0] = in[2].Path[0][:5] }), "input 2 (tree index 2): proof sibling must be 32 bytes, got 5"},
		{"adjacent_both_bad", tamper(func(in []merkle.ProofInput) { in[8].Leaf[1]++; in[9].Leaf[1]++ }), "input 8 (tree index 8): root mismatch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := merkle.RootFromProofs(tc.in, 1)
			require.EqualError(t, err, tc.want)
			// parallel chunks may report a different (but still bad) input
			_, err = merkle.RootFromProofs(tc.in, 4)
			require.Error(t, err)
		})
	}

	_, err := merkle.RootFromProofs(nil, 4)
	require.Error(t, err, "empty batch")
}
