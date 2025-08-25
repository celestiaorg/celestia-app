package merkle

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"testing"
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
			tree := NewTree(leaves)

			if !tt.wantPanic {
				if tree == nil {
					t.Error("NewTree returned nil")
				}
				if tree.numLeaves() != tt.numLeaves {
					t.Errorf("tree.numLeaves() = %d, want %d", tree.numLeaves(), tt.numLeaves)
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
			tree1 := NewTree(leaves)
			tree2 := NewTree(leaves)

			root1 := tree1.Root()
			root2 := tree2.Root()

			// Test determinism
			if !bytes.Equal(root1[:], root2[:]) {
				t.Error("Tree root is not deterministic")
			}

			// Test that root changes with different data
			leaves2 := makeTestLeaves(tt.numLeaves)
			leaves2[0][0] ^= 1 // Modify first byte
			tree3 := NewTree(leaves2)
			root3 := tree3.Root()

			if bytes.Equal(root1[:], root3[:]) {
				t.Error("Tree root did not change with different data")
			}
		})
	}
}

func TestGenerateProof(t *testing.T) {
	numLeaves := 8
	leaves := makeTestLeaves(numLeaves)
	tree := NewTree(leaves)

	for i := 0; i < numLeaves; i++ {
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
			computedRoot, err := ComputeRootFromProof(leaves[i], i, proof)
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
	tree := NewTree(leaves)

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
	tree := NewTree(leaves)
	expectedRoot := tree.Root()

	for i := 0; i < numLeaves; i++ {
		proof, err := tree.GenerateProof(i)
		if err != nil {
			t.Fatalf("GenerateProof(%d) error: %v", i, err)
		}

		// Test correct proof
		computedRoot, err := ComputeRootFromProof(leaves[i], i, proof)
		if err != nil {
			t.Fatalf("ComputeRootFromProof error: %v", err)
		}

		if !bytes.Equal(expectedRoot[:], computedRoot[:]) {
			t.Errorf("Index %d: computed root doesn't match", i)
		}

		// Test wrong index
		wrongIndex := (i + 1) % numLeaves
		wrongRoot, _ := ComputeRootFromProof(leaves[i], wrongIndex, proof)
		if bytes.Equal(expectedRoot[:], wrongRoot[:]) {
			t.Errorf("Index %d: proof should fail with wrong index", i)
		}

		// Test wrong leaf
		wrongLeaf := make([]byte, 32)
		copy(wrongLeaf, leaves[i])
		wrongLeaf[0] ^= 1
		wrongRoot, _ = ComputeRootFromProof(wrongLeaf, i, proof)
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
		{"k4_n4", 8, 4, false, 1},      // 4 original, 4 parity
		{"k4_n12", 16, 4, false, 2},    // 4 original, 12 parity
		{"k8_n8", 16, 8, false, 1},     // 8 original, 8 parity
		{"k16_n48", 64, 16, false, 2},  // 16 original, 48 parity
		{"k32_n32", 64, 32, false, 1},  // 32 original, 32 parity
		{"invalid_k0", 8, 0, true, 0},
		{"invalid_k_equals_n", 8, 8, true, 0},
		{"invalid_k_not_power", 8, 3, true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			leaves := makeTestLeaves(tt.numLeaves)
			tree := NewTree(leaves)

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
			leftTree := NewTree(leftLeaves)
			leftRoot := leftTree.Root()

			// Use the proof to compute the full root
			computedRoot := ComputeRootFromLeftSubtreeProof(leftRoot, proof)
			expectedRoot := tree.Root()

			if !bytes.Equal(expectedRoot[:], computedRoot[:]) {
				t.Error("Left subtree proof verification failed")
			}
		})
	}
}

func TestTreeDepth(t *testing.T) {
	tests := []struct {
		numLeaves int
		wantDepth int
	}{
		{1, 0},
		{2, 1},
		{4, 2},
		{8, 3},
		{16, 4},
		{32, 5},
		{64, 6},
		{128, 7},
		{256, 8},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("leaves_%d", tt.numLeaves), func(t *testing.T) {
			leaves := makeTestLeaves(tt.numLeaves)
			tree := NewTree(leaves)
			
			depth := tree.depth()
			if depth != tt.wantDepth {
				t.Errorf("depth() = %d, want %d", depth, tt.wantDepth)
			}
		})
	}
}

func TestHashPair(t *testing.T) {
	left := []byte("left data")
	right := []byte("right data")
	
	// Test that hashPair is deterministic
	hash1 := hashPair(left, right)
	hash2 := hashPair(left, right)
	
	if !bytes.Equal(hash1, hash2) {
		t.Error("hashPair is not deterministic")
	}
	
	// Test that order matters
	hash3 := hashPair(right, left)
	if bytes.Equal(hash1, hash3) {
		t.Error("hashPair(left, right) should differ from hashPair(right, left)")
	}
	
	// Test expected length
	if len(hash1) != 32 {
		t.Errorf("hashPair returned %d bytes, expected 32", len(hash1))
	}
	
	// Test with actual SHA256 (now includes inner prefix)
	h := sha256.New()
	h.Write(innerPrefix)
	h.Write(left)
	h.Write(right)
	expected := h.Sum(nil)
	
	if !bytes.Equal(hash1, expected) {
		t.Error("hashPair does not match expected SHA256 output")
	}
}

func TestHashLeaf(t *testing.T) {
	data := []byte("leaf data")
	
	// Test that hashLeaf is deterministic
	hash1 := hashLeaf(data)
	hash2 := hashLeaf(data)
	
	if !bytes.Equal(hash1, hash2) {
		t.Error("hashLeaf is not deterministic")
	}
	
	// Test expected length
	if len(hash1) != 32 {
		t.Errorf("hashLeaf returned %d bytes, expected 32", len(hash1))
	}
	
	// Test with actual SHA256 (includes leaf prefix)
	h := sha256.New()
	h.Write(leafPrefix)
	h.Write(data)
	expected := h.Sum(nil)
	
	if !bytes.Equal(hash1, expected) {
		t.Error("hashLeaf does not match expected SHA256 output")
	}
	
	// Test that hashLeaf differs from raw hash
	h2 := sha256.New()
	h2.Write(data)
	rawHash := h2.Sum(nil)
	
	if bytes.Equal(hash1, rawHash) {
		t.Error("hashLeaf should differ from raw SHA256 due to leaf prefix")
	}
}

// Helper functions

func makeTestLeaves(n int) [][]byte {
	leaves := make([][]byte, n)
	for i := 0; i < n; i++ {
		leaf := make([]byte, 32)
		// Fill with pattern based on index
		for j := 0; j < 32; j++ {
			leaf[j] = byte((i + j) % 256)
		}
		leaves[i] = leaf
	}
	return leaves
}