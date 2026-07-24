package merkle_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/merkle"
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

func TestRootFromFunc(t *testing.T) {
	for _, numLeaves := range []int{1, 2, 4, 16, 256} {
		t.Run(fmt.Sprintf("leaves_%d", numLeaves), func(t *testing.T) {
			leaves := makeTestLeaves(numLeaves)
			treeRoot := merkle.NewTree(leaves, 1).Root()
			buf := make([]byte, numLeaves*merkle.NodeSize)
			root := merkle.RootFromFunc(buf, func(i int, dst []byte) []byte {
				return leaves[i]
			})
			if !bytes.Equal(root[:], treeRoot[:]) {
				t.Fatalf("root mismatch: got %x want %x", root, treeRoot)
			}
		})
	}
}

// TestRootFromFuncRecyclesDst verifies the tree threads each leaf's returned
// slice back as the next dst, so a serializing callback allocates only once.
func TestRootFromFuncRecyclesDst(t *testing.T) {
	leaves := makeTestLeaves(256)
	buf := make([]byte, 256*merkle.NodeSize)
	allocs := 0
	merkle.RootFromFunc(buf, func(i int, dst []byte) []byte {
		if cap(dst) < 32 {
			allocs++
			dst = make([]byte, 32)
		}
		dst = dst[:32]
		copy(dst, leaves[i])
		return dst
	})
	if allocs != 1 {
		t.Fatalf("callback allocated %d times, want 1 (dst should be recycled)", allocs)
	}
}

func TestNewTreeInto(t *testing.T) {
	for _, numLeaves := range []int{1, 2, 4, 16, 256} {
		t.Run(fmt.Sprintf("leaves_%d", numLeaves), func(t *testing.T) {
			leaves := makeTestLeaves(numLeaves)
			want := merkle.NewTree(leaves, 4).Root()
			buf := make([]byte, merkle.TreeBufferSize(numLeaves))
			got := merkle.NewTreeInto(buf, leaves, 4).Root()

			if !bytes.Equal(got[:], want[:]) {
				t.Fatalf("root mismatch: got %x want %x", got, want)
			}
		})
	}
}

func TestNewTreeIntoLeafCountMismatch(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewTreeInto with mismatched leaf count should panic")
		}
	}()
	buf := make([]byte, merkle.TreeBufferSize(8))
	merkle.NewTreeInto(buf, makeTestLeaves(4), 1)
}

func TestNewTreeFuncInto(t *testing.T) {
	for _, numLeaves := range []int{1, 2, 4, 16, 256} {
		t.Run(fmt.Sprintf("leaves_%d", numLeaves), func(t *testing.T) {
			leaves := makeTestLeaves(numLeaves)
			want := merkle.NewTree(leaves, 4).Root()
			buf := make([]byte, merkle.TreeBufferSize(numLeaves))
			got := merkle.NewTreeFuncInto(buf, 4, func(i int, _ []byte) []byte {
				return leaves[i]
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
	var buf [8 * merkle.NodeSize]byte
	rootAllocs := testing.AllocsPerRun(100, func() {
		root = merkle.RootFromFunc(buf[:], func(i int, dst []byte) []byte {
			return leaves[i]
		})
	})
	if rootAllocs != 0 {
		t.Fatalf("RootFromFunc allocated %.0f times", rootAllocs)
	}
	if root == ([32]byte{}) {
		t.Fatal("unexpected zero root")
	}
}

func TestProof(t *testing.T) {
	numLeaves := 8
	leaves := makeTestLeaves(numLeaves)
	tree := merkle.NewTree(leaves, 1)

	for i := range numLeaves {
		t.Run(fmt.Sprintf("leaf_%d", i), func(t *testing.T) {
			proof, err := tree.Proof(i)
			if err != nil {
				t.Fatalf("Proof(%d) error: %v", i, err)
			}

			// Proof length should be log2(numLeaves)
			expectedLen := 3 // log2(8) = 3
			if len(proof) != expectedLen {
				t.Errorf("Proof length = %d, want %d", len(proof), expectedLen)
			}

			// Verify the proof works
			root := tree.Root()
			computedRoot, err := merkle.RootFromProof(leaves[i], i, proof)
			if err != nil {
				t.Fatalf("RootFromProof error: %v", err)
			}

			if !bytes.Equal(root[:], computedRoot[:]) {
				t.Error("Proof verification failed")
			}
		})
	}
}

func TestProofErrors(t *testing.T) {
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
			_, err := tree.Proof(tt.index)
			if err == nil {
				t.Errorf("Proof(%d) should return error", tt.index)
			}
		})
	}
}

func TestRootFromProof(t *testing.T) {
	// Build a tree and generate proofs
	numLeaves := 16
	leaves := makeTestLeaves(numLeaves)
	tree := merkle.NewTree(leaves, 1)
	expectedRoot := tree.Root()

	for i := range numLeaves {
		proof, err := tree.Proof(i)
		if err != nil {
			t.Fatalf("Proof(%d) error: %v", i, err)
		}

		// Test correct proof
		computedRoot, err := merkle.RootFromProof(leaves[i], i, proof)
		if err != nil {
			t.Fatalf("RootFromProof error: %v", err)
		}

		if !bytes.Equal(expectedRoot[:], computedRoot[:]) {
			t.Errorf("Index %d: computed root doesn't match", i)
		}

		// Test wrong index
		wrongIndex := (i + 1) % numLeaves
		wrongRoot, _ := merkle.RootFromProof(leaves[i], wrongIndex, proof)
		if bytes.Equal(expectedRoot[:], wrongRoot[:]) {
			t.Errorf("Index %d: proof should fail with wrong index", i)
		}

		// Test wrong leaf
		wrongLeaf := make([]byte, 32)
		copy(wrongLeaf, leaves[i])
		wrongLeaf[0] ^= 1
		wrongRoot, _ = merkle.RootFromProof(wrongLeaf, i, proof)
		if bytes.Equal(expectedRoot[:], wrongRoot[:]) {
			t.Errorf("Index %d: proof should fail with wrong leaf", i)
		}

		// A sibling padded past NodeSize must be rejected, not silently
		// truncated back to a valid 32-byte prefix.
		padded := make([][]byte, len(proof))
		copy(padded, proof)
		padded[0] = append(append([]byte(nil), proof[0]...), 0xAA, 0xBB)
		_, err = merkle.RootFromProof(leaves[i], i, padded)
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("proof sibling must be %d bytes, got %d", merkle.NodeSize, merkle.NodeSize+2)) {
			t.Errorf("Index %d: expected oversized-sibling error, got %v", i, err)
		}
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
