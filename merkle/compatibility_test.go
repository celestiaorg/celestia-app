package merkle

import (
	"bytes"
	"fmt"
	"testing"

	cmtmerkle "github.com/cometbft/cometbft/crypto/merkle"
)

// TestCometBFTCompatibility tests that our merkle tree implementation
// produces the same roots as CometBFT/Celestia-core's implementation
func TestCometBFTCompatibility(t *testing.T) {
	tests := []struct {
		name      string
		numLeaves int
	}{
		{"single_leaf", 1},
		{"two_leaves", 2},
		{"four_leaves", 4},
		{"eight_leaves", 8},
		{"sixteen_leaves", 16},
		{"thirty_two_leaves", 32},
		{"sixty_four_leaves", 64},
		{"one_twenty_eight_leaves", 128},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test data
			leaves := makeTestLeaves(tt.numLeaves)
			
			// Our implementation
			ourTree := NewTree(leaves)
			ourRoot := ourTree.Root()
			
			// CometBFT implementation
			cometRoot := cmtmerkle.HashFromByteSlices(leaves)
			
			// Compare roots
			if !bytes.Equal(ourRoot[:], cometRoot) {
				t.Errorf("Root mismatch for %d leaves:\nOur root:   %x\nComet root: %x", 
					tt.numLeaves, ourRoot, cometRoot)
			}
		})
	}
}

// TestCometBFTProofCompatibility tests that our proof generation and verification
// works correctly alongside CometBFT's implementation
func TestCometBFTProofCompatibility(t *testing.T) {
	numLeaves := 8
	leaves := makeTestLeaves(numLeaves)
	
	// Build our tree
	ourTree := NewTree(leaves)
	ourRoot := ourTree.Root()
	
	// CometBFT should produce the same root
	cometRoot := cmtmerkle.HashFromByteSlices(leaves)
	
	if !bytes.Equal(ourRoot[:], cometRoot) {
		t.Fatalf("Root mismatch before proof testing")
	}
	
	// Test proof generation and verification for each leaf
	for i := 0; i < numLeaves; i++ {
		t.Run(fmt.Sprintf("leaf_%d", i), func(t *testing.T) {
			// Generate our proof
			ourProof, err := ourTree.GenerateProof(i)
			if err != nil {
				t.Fatalf("Our GenerateProof error: %v", err)
			}
			
			// Verify our proof produces the correct root
			computedRoot, err := ComputeRootFromProof(leaves[i], i, ourProof)
			if err != nil {
				t.Fatalf("ComputeRootFromProof error: %v", err)
			}
			
			if !bytes.Equal(ourRoot[:], computedRoot[:]) {
				t.Errorf("Our proof verification failed for index %d", i)
			}
			
			// Also verify the proof produces the same root as CometBFT
			if !bytes.Equal(computedRoot[:], cometRoot) {
				t.Errorf("Computed root doesn't match CometBFT root for index %d", i)
			}
		})
	}
}

// TestCometBFTEdgeCases tests edge cases and special scenarios
func TestCometBFTEdgeCases(t *testing.T) {
	// Test with different leaf sizes
	tests := []struct {
		name     string
		leafSize int
		numLeaves int
	}{
		{"small_leaves", 16, 4},
		{"standard_leaves", 32, 4},
		{"large_leaves", 64, 4},
		{"mixed_standard", 32, 16},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create leaves of specified size
			leaves := make([][]byte, tt.numLeaves)
			for i := 0; i < tt.numLeaves; i++ {
				leaf := make([]byte, tt.leafSize)
				for j := 0; j < tt.leafSize; j++ {
					leaf[j] = byte((i * tt.leafSize + j) % 256)
				}
				leaves[i] = leaf
			}
			
			// Our implementation
			ourTree := NewTree(leaves)
			ourRoot := ourTree.Root()
			
			// CometBFT implementation
			cometRoot := cmtmerkle.HashFromByteSlices(leaves)
			
			// Compare
			if !bytes.Equal(ourRoot[:], cometRoot) {
				t.Errorf("Root mismatch for %s:\nOur root:   %x\nComet root: %x", 
					tt.name, ourRoot, cometRoot)
			}
		})
	}
}

// TestCometBFTEmptyAndNilLeaves tests handling of empty and nil leaves
func TestCometBFTEmptyAndNilLeaves(t *testing.T) {
	tests := []struct {
		name   string
		leaves [][]byte
	}{
		{
			name:   "empty_leaves",
			leaves: [][]byte{{}, {}, {}, {}},
		},
		{
			name:   "mixed_empty_and_data",
			leaves: [][]byte{{1, 2, 3}, {}, {4, 5, 6}, {}},
		},
		{
			name:   "all_zeros",
			leaves: [][]byte{{0, 0, 0, 0}, {0, 0, 0, 0}, {0, 0, 0, 0}, {0, 0, 0, 0}},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Our implementation
			ourTree := NewTree(tt.leaves)
			ourRoot := ourTree.Root()
			
			// CometBFT implementation
			cometRoot := cmtmerkle.HashFromByteSlices(tt.leaves)
			
			// Compare
			if !bytes.Equal(ourRoot[:], cometRoot) {
				t.Errorf("Root mismatch for %s:\nOur root:   %x\nComet root: %x", 
					tt.name, ourRoot, cometRoot)
			}
		})
	}
}