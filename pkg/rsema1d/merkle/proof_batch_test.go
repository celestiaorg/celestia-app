package merkle

import (
	"encoding/hex"
	"errors"
	"math/rand/v2"
	"strings"
	"testing"
)

// proofInputs builds a tree over leaves and returns one ProofInput per leaf.
func proofInputs(t testing.TB, leaves [][]byte) (*Tree, []ProofInput) {
	t.Helper()
	tree := NewTree(leaves, 1)
	inputs := make([]ProofInput, len(leaves))
	for i := range leaves {
		path, err := tree.Proof(i)
		if err != nil {
			t.Fatal(err)
		}
		inputs[i] = ProofInput{Leaf: leaves[i], Index: i, Path: path}
	}
	return tree, inputs
}

func TestRootFromProofsMatchesRootFromProof(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 12))
	for _, n := range []int{1, 2, 4, 8, 16, 64, 1024} {
		for _, leafSize := range []int{0, 1, 62, 63, 64, 65, 127, 128, 512, 4160, 32768} {
			leaves := make([][]byte, n)
			for i := range leaves {
				leaves[i] = randBytes(rng, leafSize)
			}
			tree, inputs := proofInputs(t, leaves)
			for _, workers := range []int{1, 4, 16} {
				// every subrange, so both the paired and the odd-tail path are hit
				for start := 0; start < n && start < 8; start++ {
					got, err := RootFromProofs(inputs[start:], workers)
					if err != nil {
						t.Fatalf("n=%d leafSize=%d start=%d: %v", n, leafSize, start, err)
					}
					if got != tree.Root() {
						t.Fatalf("n=%d leafSize=%d start=%d: root mismatch", n, leafSize, start)
					}
				}
			}
			for _, in := range inputs {
				single, err := RootFromProof(in.Leaf, in.Index, in.Path)
				if err != nil || single != tree.Root() {
					t.Fatalf("RootFromProof %d: %v", in.Index, err)
				}
			}
		}
	}
}

// TestRootFromProofsPinned pins the batch root for a fixed input set, captured
// from the single-stream implementation.
func TestRootFromProofsPinned(t *testing.T) {
	leaves := make([][]byte, 64)
	for i := range leaves {
		leaves[i] = make([]byte, 32768)
		for j := range leaves[i] {
			leaves[i][j] = byte(i*7 + j)
		}
	}
	_, inputs := proofInputs(t, leaves)
	got, err := RootFromProofs(inputs, 4)
	if err != nil {
		t.Fatal(err)
	}
	const want = "d60a298d50c078ab36e021e25c407c1a89e1b94e7333099aa642251be551bcd1"
	if hex.EncodeToString(got[:]) != want {
		t.Fatalf("root %x, want %s", got[:], want)
	}
}

func TestRootFromProofsUnequalLeafLengths(t *testing.T) {
	rng := rand.New(rand.NewPCG(13, 14))
	leaves := make([][]byte, 8)
	for i := range leaves {
		leaves[i] = randBytes(rng, []int{0, 1, 63, 64, 65, 100, 200, 32768}[i])
	}
	tree, inputs := proofInputs(t, leaves)
	for _, workers := range []int{1, 4} {
		got, err := RootFromProofs(inputs, workers)
		if err != nil || got != tree.Root() {
			t.Fatalf("workers=%d: err=%v root mismatch", workers, err)
		}
	}
}

func TestRootFromProofsErrors(t *testing.T) {
	rng := rand.New(rand.NewPCG(15, 16))
	const n = 16
	leaves := make([][]byte, n)
	for i := range leaves {
		leaves[i] = randBytes(rng, 256)
	}
	_, inputs := proofInputs(t, leaves)

	tamper := func(mod func(in []ProofInput)) []ProofInput {
		cp := make([]ProofInput, n)
		for i := range inputs {
			cp[i] = inputs[i]
			cp[i].Leaf = append([]byte(nil), inputs[i].Leaf...)
			cp[i].Path = make([][]byte, len(inputs[i].Path))
			for j := range inputs[i].Path {
				cp[i].Path[j] = append([]byte(nil), inputs[i].Path[j]...)
			}
		}
		mod(cp)
		return cp
	}
	cases := []struct {
		name string
		in   []ProofInput
		want string
	}{
		{"tampered_leaf_even", tamper(func(in []ProofInput) { in[4].Leaf[0]++ }), "input 4 (tree index 4): root mismatch"},
		{"tampered_leaf_odd", tamper(func(in []ProofInput) { in[5].Leaf[0]++ }), "input 5 (tree index 5): root mismatch"},
		{"tampered_first", tamper(func(in []ProofInput) { in[0].Leaf[0]++ }), "input 1 (tree index 1): root mismatch"},
		{"tampered_last", tamper(func(in []ProofInput) { in[15].Leaf[0]++ }), "input 15 (tree index 15): root mismatch"},
		{"tampered_sibling", tamper(func(in []ProofInput) { in[6].Path[1][3]++ }), "input 6 (tree index 6): root mismatch"},
		{"wrong_index", tamper(func(in []ProofInput) { in[7].Index = 6 }), "input 7 (tree index 6): root mismatch"},
		{"short_sibling", tamper(func(in []ProofInput) { in[3].Path[0] = in[3].Path[0][:31] }), "input 3 (tree index 3): proof sibling must be 32 bytes, got 31"},
		{"short_sibling_first", tamper(func(in []ProofInput) { in[0].Path[2] = nil }), "input 0 (tree index 0): proof sibling must be 32 bytes, got 0"},
		{"short_path", tamper(func(in []ProofInput) { in[9].Path = in[9].Path[:2] }), "input 9 (tree index 9): root mismatch"},
		{"first_bad_wins", tamper(func(in []ProofInput) { in[10].Leaf[1]++; in[2].Path[0] = in[2].Path[0][:5] }), "input 2 (tree index 2): proof sibling must be 32 bytes, got 5"},
		{"pair_both_bad", tamper(func(in []ProofInput) { in[8].Leaf[1]++; in[9].Leaf[1]++ }), "input 8 (tree index 8): root mismatch"},
	}
	for _, tc := range cases {
		for _, workers := range []int{1, 4} {
			_, err := RootFromProofs(tc.in, workers)
			if err == nil {
				t.Fatalf("%s workers=%d: expected error", tc.name, workers)
			}
			if workers == 1 && err.Error() != tc.want {
				t.Fatalf("%s: got %q, want %q", tc.name, err, tc.want)
			}
			if workers > 1 && !strings.Contains(err.Error(), "mismatch") && !strings.Contains(err.Error(), "sibling") {
				t.Fatalf("%s workers=%d: unexpected error %q", tc.name, workers, err)
			}
		}
	}
	if _, err := RootFromProofs(nil, 4); err == nil {
		t.Fatal("expected error for no inputs")
	}
}

func FuzzRootFromProofs(f *testing.F) {
	rng := rand.New(rand.NewPCG(17, 18))
	leaves := make([][]byte, 8)
	for i := range leaves {
		leaves[i] = randBytes(rng, 100)
	}
	_, inputs := proofInputs(f, leaves)
	f.Add(0, 0, byte(1), 7)
	f.Add(3, 2, byte(0), 0)
	f.Fuzz(func(t *testing.T, which, level int, delta byte, trunc int) {
		cp := make([]ProofInput, len(inputs))
		copy(cp, inputs)
		i := which & 7
		cp[i].Leaf = append([]byte(nil), inputs[i].Leaf...)
		cp[i].Path = append([][]byte(nil), inputs[i].Path...)
		if delta != 0 {
			cp[i].Leaf[int(delta)%len(cp[i].Leaf)] += delta
		}
		l := level % len(cp[i].Path)
		if l < 0 {
			l = -l
		}
		cp[i].Path[l] = append([]byte(nil), inputs[i].Path[l]...)[:max(0, min(NodeSize, trunc))]

		// reference: every proof verifies on its own and all agree
		want, wantErr := RootFromProof(cp[0].Leaf, cp[0].Index, cp[0].Path)
		for _, in := range cp[1:] {
			if wantErr != nil {
				break
			}
			r, err := RootFromProof(in.Leaf, in.Index, in.Path)
			if err != nil {
				wantErr = err
			} else if r != want {
				wantErr = errMismatch
			}
		}
		got, err := RootFromProofs(cp, 1)
		if (err != nil) != (wantErr != nil) {
			t.Fatalf("err mismatch: got %v, want %v", err, wantErr)
		}
		if err == nil && got != want {
			t.Fatal("root mismatch")
		}
	})
}

var errMismatch = errors.New("root mismatch")
