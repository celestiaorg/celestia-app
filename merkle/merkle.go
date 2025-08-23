package merkle

import (
	"crypto/sha256"
	"errors"
	"fmt"
)

// Tree represents a binary Merkle tree
type Tree struct {
	leaves [][]byte
	nodes  [][]byte // Internal nodes (level 0 = leaves, level h = root)
	depth  int
}

// NewTree builds a binary Merkle tree from the given leaves
func NewTree(leaves [][]byte) *Tree {
	if len(leaves) == 0 {
		panic("cannot create Merkle tree with 0 leaves")
	}

	// Pad to power of 2
	n := nextPowerOfTwo(len(leaves))
	paddedLeaves := make([][]byte, n)
	copy(paddedLeaves, leaves)

	// Pad with hash of empty data for missing leaves
	emptyHash := sha256.Sum256(nil)
	for i := len(leaves); i < n; i++ {
		paddedLeaves[i] = emptyHash[:]
	}

	// Calculate tree depth
	depth := 0
	for size := n; size > 1; size >>= 1 {
		depth++
	}

	// Build tree bottom-up
	nodes := make([][]byte, 2*n-1)
	
	// Copy leaves to the end of the nodes array
	for i := 0; i < n; i++ {
		nodes[n-1+i] = paddedLeaves[i]
	}

	// Build internal nodes
	for i := n - 2; i >= 0; i-- {
		left := nodes[2*i+1]
		right := nodes[2*i+2]
		nodes[i] = hashPair(left, right)
	}

	return &Tree{
		leaves: paddedLeaves,
		nodes:  nodes,
		depth:  depth,
	}
}

// Root returns the Merkle root
func (t *Tree) Root() [32]byte {
	var root [32]byte
	copy(root[:], t.nodes[0])
	return root
}

// GenerateProof generates a Merkle proof for the leaf at the given index
func (t *Tree) GenerateProof(index int) ([][]byte, error) {
	if index < 0 || index >= len(t.leaves) {
		return nil, fmt.Errorf("index %d out of range [0, %d)", index, len(t.leaves))
	}

	proof := make([][]byte, 0, t.depth)
	n := len(t.leaves)
	pos := n - 1 + index

	// Traverse from leaf to root
	for pos > 0 {
		// Find sibling
		var sibling int
		if pos%2 == 1 {
			sibling = pos + 1 // Right sibling
		} else {
			sibling = pos - 1 // Left sibling
		}
		
		if sibling < len(t.nodes) {
			proof = append(proof, t.nodes[sibling])
		}
		
		// Move to parent
		pos = (pos - 1) / 2
	}

	return proof, nil
}

// VerifyProof verifies a Merkle proof
func VerifyProof(leaf []byte, index int, proof [][]byte, root [32]byte) error {
	if index < 0 {
		return errors.New("index must be non-negative")
	}

	// Start with the leaf
	current := leaf
	pos := index

	// Traverse up the tree
	for _, sibling := range proof {
		if pos%2 == 0 {
			// Current is left child
			current = hashPair(current, sibling)
		} else {
			// Current is right child
			current = hashPair(sibling, current)
		}
		pos /= 2
	}

	// Check if computed root matches expected root
	var computedRoot [32]byte
	copy(computedRoot[:], current)
	if computedRoot != root {
		return errors.New("proof verification failed: root mismatch")
	}

	return nil
}

// ComputeSubtreeRoot computes the Merkle root of a subtree from its leaves
func ComputeSubtreeRoot(leaves [][]byte) [32]byte {
	if len(leaves) == 0 {
		return sha256.Sum256(nil)
	}
	if len(leaves) == 1 {
		var root [32]byte
		copy(root[:], leaves[0])
		return root
	}

	tree := NewTree(leaves)
	return tree.Root()
}

// hashPair computes SHA256(left || right)
func hashPair(left, right []byte) []byte {
	h := sha256.New()
	h.Write(left)
	h.Write(right)
	return h.Sum(nil)
}

// nextPowerOfTwo returns the next power of 2 >= n
func nextPowerOfTwo(n int) int {
	if n <= 1 {
		return 1
	}
	power := 1
	for power < n {
		power <<= 1
	}
	return power
}