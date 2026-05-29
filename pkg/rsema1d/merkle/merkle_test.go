package merkle_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/merkle"
)

func TestNewTree(t *testing.T) {
	tests := []struct {
		name      string
		numLeaves int
		wantPanic bool
	}{
		{"single_leaf", 1, false},
		{"two_leaves", 2, false},
		{"four_leaves", 4, false},
		{"eight_leaves", 8, false},
		{"power_of_two", 64, false},
		{"not_power_of_two", 3, true},
		{"not_power_of_two_2", 5, true},
		{"zero_leaves", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantPanic {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("NewTree with %d leaves should panic", tt.numLeaves)
					}
				}()
			}

			leaves := makeTestLeaves(tt.numLeaves)
			tree := merkle.NewTree(leaves, 1)

			if !tt.wantPanic {
				if tree == nil {
					t.Error("NewTree returned nil")
				}
			}
		})
	}
}

func TestTreeRoot(t *testing.T) {
	tests := []struct {
		name      string
		numLeaves int
	}{
		{"single", 1},
		{"two", 2},
		{"four", 4},
		{"eight", 8},
		{"sixteen", 16},
		{"large", 256},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			leaves := makeTestLeaves(tt.numLeaves)
			tree1 := merkle.NewTree(leaves, 1)
			tree2 := merkle.NewTree(leaves, 1)

			root1 := tree1.Root()
			root2 := tree2.Root()

			// Test determinism
			if !bytes.Equal(root1[:], root2[:]) {
				t.Error("Tree root is not deterministic")
			}

			// Test that root changes with different data
			leaves2 := makeTestLeaves(tt.numLeaves)
			leaves2[0][0] ^= 1 // Modify first byte
			tree3 := merkle.NewTree(leaves2, 1)
			root3 := tree3.Root()

			if bytes.Equal(root1[:], root3[:]) {
				t.Error("Tree root did not change with different data")
			}
		})
	}
}

func TestComputeRootFromWriter(t *testing.T) {
	for _, numLeaves := range []int{1, 2, 4, 16, 256} {
		t.Run(fmt.Sprintf("leaves_%d", numLeaves), func(t *testing.T) {
			leaves := makeTestLeaves(numLeaves)
			treeRoot := merkle.NewTree(leaves, 1).Root()
			scratch := make([][32]byte, numLeaves)
			leafScratch := make([]byte, len(leaves[0]))
			root := merkle.ComputeRootFromWriter(scratch, leafScratch, numLeaves, func(i int, dst []byte) {
				copy(dst, leaves[i])
			})
			if !bytes.Equal(root[:], treeRoot[:]) {
				t.Fatalf("root mismatch: got %x want %x", root, treeRoot)
			}
		})
	}
}

func TestNewTreeFromWriter(t *testing.T) {
	for _, numLeaves := range []int{1, 2, 4, 16, 256} {
		t.Run(fmt.Sprintf("leaves_%d", numLeaves), func(t *testing.T) {
			leaves := makeTestLeaves(numLeaves)
			want := merkle.NewTree(leaves, 4).Root()
			got := merkle.NewTreeFromWriter(numLeaves, len(leaves[0]), 4, func(i int, dst []byte) {
				copy(dst, leaves[i])
			}).Root()

			if !bytes.Equal(got[:], want[:]) {
				t.Fatalf("root mismatch: got %x want %x", got, want)
			}
		})
	}
}

func TestCallerOwnedStorageDoesNotAllocate(t *testing.T) {
	leaves := makeTestLeaves(8)

	var root [32]byte
	var scratch [8][32]byte
	var leafScratch [32]byte
	rootAllocs := testing.AllocsPerRun(100, func() {
		root = merkle.ComputeRootFromWriter(scratch[:], leafScratch[:], len(leaves), func(i int, dst []byte) {
			copy(dst, leaves[i])
		})
	})
	if rootAllocs != 0 {
		t.Fatalf("ComputeRootFromWriter allocated %.0f times", rootAllocs)
	}
	if root == ([32]byte{}) {
		t.Fatal("unexpected zero root")
	}
}

func TestGenerateProof(t *testing.T) {
	numLeaves := 8
	leaves := makeTestLeaves(numLeaves)
	tree := merkle.NewTree(leaves, 1)

	for i := range numLeaves {
		t.Run(fmt.Sprintf("leaf_%d", i), func(t *testing.T) {
			proof, err := tree.GenerateProof(i)
			if err != nil {
				t.Fatalf("GenerateProof(%d) error: %v", i, err)
			}

			// Proof length should be log2(numLeaves)
			expectedLen := 3 // log2(8) = 3
			if len(proof) != expectedLen {
				t.Errorf("Proof length = %d, want %d", len(proof), expectedLen)
			}

			// Verify the proof works
			root := tree.Root()
			computedRoot, err := merkle.ComputeRootFromProof(leaves[i], i, proof)
			if err != nil {
				t.Fatalf("ComputeRootFromProof error: %v", err)
			}

			if !bytes.Equal(root[:], computedRoot[:]) {
				t.Error("Proof verification failed")
			}
		})
	}
}

func TestGenerateProofErrors(t *testing.T) {
	leaves := makeTestLeaves(8)
	tree := merkle.NewTree(leaves, 1)

	tests := []struct {
		name  string
		index int
	}{
		{"negative_index", -1},
		{"index_too_large", 8},
		{"index_way_too_large", 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tree.GenerateProof(tt.index)
			if err == nil {
				t.Errorf("GenerateProof(%d) should return error", tt.index)
			}
		})
	}
}

func TestComputeRootFromProof(t *testing.T) {
	// Build a tree and generate proofs
	numLeaves := 16
	leaves := makeTestLeaves(numLeaves)
	tree := merkle.NewTree(leaves, 1)
	expectedRoot := tree.Root()

	for i := range numLeaves {
		proof, err := tree.GenerateProof(i)
		if err != nil {
			t.Fatalf("GenerateProof(%d) error: %v", i, err)
		}

		// Test correct proof
		computedRoot, err := merkle.ComputeRootFromProof(leaves[i], i, proof)
		if err != nil {
			t.Fatalf("ComputeRootFromProof error: %v", err)
		}

		if !bytes.Equal(expectedRoot[:], computedRoot[:]) {
			t.Errorf("Index %d: computed root doesn't match", i)
		}

		// Test wrong index
		wrongIndex := (i + 1) % numLeaves
		wrongRoot, _ := merkle.ComputeRootFromProof(leaves[i], wrongIndex, proof)
		if bytes.Equal(expectedRoot[:], wrongRoot[:]) {
			t.Errorf("Index %d: proof should fail with wrong index", i)
		}

		// Test wrong leaf
		wrongLeaf := make([]byte, 32)
		copy(wrongLeaf, leaves[i])
		wrongLeaf[0] ^= 1
		wrongRoot, _ = merkle.ComputeRootFromProof(wrongLeaf, i, proof)
		if bytes.Equal(expectedRoot[:], wrongRoot[:]) {
			t.Errorf("Index %d: proof should fail with wrong leaf", i)
		}
	}
}

func TestGenerateLeftSubtreeProof(t *testing.T) {
	tests := []struct {
		name      string
		numLeaves int
		k         int
		wantErr   bool
		proofLen  int
	}{
		{"k4_n4", 8, 4, false, 1},     // 4 original, 4 parity
		{"k4_n12", 16, 4, false, 2},   // 4 original, 12 parity
		{"k8_n8", 16, 8, false, 1},    // 8 original, 8 parity
		{"k16_n48", 64, 16, false, 2}, // 16 original, 48 parity
		{"k32_n32", 64, 32, false, 1}, // 32 original, 32 parity
		{"invalid_k0", 8, 0, true, 0},
		{"invalid_k_equals_n", 8, 8, true, 0},
		{"invalid_k_not_power", 8, 3, true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			leaves := makeTestLeaves(tt.numLeaves)
			tree := merkle.NewTree(leaves, 1)

			proof, err := tree.GenerateLeftSubtreeProof(tt.k)
			if tt.wantErr {
				if err == nil {
					t.Errorf("GenerateLeftSubtreeProof(%d) should return error", tt.k)
				}
				return
			}

			if err != nil {
				t.Fatalf("GenerateLeftSubtreeProof(%d) error: %v", tt.k, err)
			}

			if len(proof) != tt.proofLen {
				t.Errorf("Proof length = %d, want %d", len(proof), tt.proofLen)
			}

			// Verify the proof works
			// Compute the left subtree root manually
			leftLeaves := leaves[:tt.k]
			leftTree := merkle.NewTree(leftLeaves, 1)
			leftRoot := leftTree.Root()

			// Use the proof to compute the full root
			computedRoot := merkle.ComputeRootFromLeftSubtreeProof(leftRoot, proof)
			expectedRoot := tree.Root()

			if !bytes.Equal(expectedRoot[:], computedRoot[:]) {
				t.Error("Left subtree proof verification failed")
			}
		})
	}
}

// makeTestLeaves builds n 32-byte leaves with a deterministic per-index pattern.
// Shared across the external merkle_test package files.
func makeTestLeaves(n int) [][]byte {
	leaves := make([][]byte, n)
	for i := range n {
		leaf := make([]byte, 32)
		// Fill with pattern based on index
		for j := range 32 {
			leaf[j] = byte((i + j) % 256)
		}
		leaves[i] = leaf
	}
	return leaves
}
