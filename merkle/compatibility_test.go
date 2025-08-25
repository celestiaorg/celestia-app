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

// TestCometBFTProofCrossVerification tests that our implementation
// produces proofs compatible with CometBFT's verification
func TestCometBFTProofCrossVerification(t *testing.T) {
	testCases := []int{1, 2, 4, 8, 16, 32}
	
	for _, numLeaves := range testCases {
		t.Run(fmt.Sprintf("leaves_%d", numLeaves), func(t *testing.T) {
			leaves := makeTestLeaves(numLeaves)
			
			// Build trees with both implementations
			ourTree := NewTree(leaves)
			ourRoot := ourTree.Root()
			
			// Generate all proofs with CometBFT
			cometRoot, cometProofs := cmtmerkle.ProofsFromByteSlices(leaves)
			
			// Roots should match
			if !bytes.Equal(ourRoot[:], cometRoot) {
				t.Fatalf("Root mismatch: our=%x, comet=%x", ourRoot, cometRoot)
			}
			
			// Test each leaf
			for i := 0; i < numLeaves; i++ {
				// Get CometBFT proof for this index
				var cometProof *cmtmerkle.Proof
				for _, p := range cometProofs {
					if int(p.Index) == i {
						cometProof = p
						break
					}
				}
				if cometProof == nil {
					t.Fatalf("No CometBFT proof found for index %d", i)
				}
				
				// Verify CometBFT proof with CometBFT
				err := cometProof.Verify(cometRoot, leaves[i])
				if err != nil {
					t.Fatalf("CometBFT self-verification failed for index %d: %v", i, err)
				}
				
				// Generate our proof
				ourProof, err := ourTree.GenerateProof(i)
				if err != nil {
					t.Fatalf("Our GenerateProof failed for index %d: %v", i, err)
				}
				
				// Verify our proof with our implementation
				computedRoot, err := ComputeRootFromProof(leaves[i], i, ourProof)
				if err != nil {
					t.Fatalf("Our verification failed for index %d: %v", i, err)
				}
				if !bytes.Equal(computedRoot[:], ourRoot[:]) {
					t.Errorf("Our proof verification failed for index %d", i)
				}
				
				// Cross-verify: Create a CometBFT proof from our proof
				// We need to provide the leaf hash with proper prefix
				leafHash := hashLeaf(leaves[i])
				crossCheckProof := &cmtmerkle.Proof{
					Total:    int64(numLeaves),
					Index:    int64(i),
					LeafHash: leafHash,
					Aunts:    ourProof,
				}
				
				// Verify our proof using CometBFT's verifier
				err = crossCheckProof.Verify(cometRoot, leaves[i])
				if err != nil {
					t.Errorf("Cross-verification failed for index %d: %v", i, err)
				}
				
				// Also verify that our proof aunts match CometBFT's aunts
				if len(ourProof) != len(cometProof.Aunts) {
					t.Errorf("Proof length mismatch for index %d: our=%d, comet=%d", 
						i, len(ourProof), len(cometProof.Aunts))
				} else {
					for j := range ourProof {
						if !bytes.Equal(ourProof[j], cometProof.Aunts[j]) {
							t.Errorf("Proof aunt mismatch at index %d, aunt %d", i, j)
						}
					}
				}
			}
		})
	}
}

// TestCometBFTProofSimple tests simple proof compatibility
func TestCometBFTProofSimple(t *testing.T) {
	// Simple 4-leaf test for debugging
	leaves := [][]byte{
		[]byte("leaf0"),
		[]byte("leaf1"),
		[]byte("leaf2"),
		[]byte("leaf3"),
	}
	
	// Our implementation
	ourTree := NewTree(leaves)
	ourRoot := ourTree.Root()
	
	// CometBFT implementation
	cometRoot, cometProofs := cmtmerkle.ProofsFromByteSlices(leaves)
	
	t.Logf("Our root:   %x", ourRoot)
	t.Logf("Comet root: %x", cometRoot)
	
	if !bytes.Equal(ourRoot[:], cometRoot) {
		t.Fatalf("Roots don't match")
	}
	
	// Test index 0
	ourProof, _ := ourTree.GenerateProof(0)
	cometProof := cometProofs[0]
	
	t.Logf("Our proof aunts for index 0: %d aunts", len(ourProof))
	for i, aunt := range ourProof {
		t.Logf("  Aunt %d: %x", i, aunt)
	}
	
	t.Logf("CometBFT proof aunts for index 0: %d aunts", len(cometProof.Aunts))
	for i, aunt := range cometProof.Aunts {
		t.Logf("  Aunt %d: %x", i, aunt)
	}
	
	// Verify using CometBFT
	err := cometProof.Verify(cometRoot, leaves[0])
	if err != nil {
		t.Fatalf("CometBFT verification failed: %v", err)
	}
	
	// Cross-verify our proof with CometBFT verifier
	// CometBFT expects the leaf hash to be computed with the leaf prefix
	leafHash := hashLeaf(leaves[0])
	crossProof := &cmtmerkle.Proof{
		Total:    4,
		Index:    0,
		LeafHash: leafHash,
		Aunts:    ourProof,
	}
	err = crossProof.Verify(cometRoot, leaves[0])
	if err != nil {
		t.Errorf("Cross-verification failed: %v", err)
		t.Logf("  LeafHash we provided: %x", leafHash)
		t.Logf("  CometBFT leaf hash:   %x", cometProof.LeafHash)
	} else {
		t.Log("Cross-verification succeeded!")
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