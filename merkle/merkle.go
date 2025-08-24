package merkle

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math/bits"
)

// Tree represents a binary Merkle tree
type Tree struct {
	nodes [][]byte // All nodes: [root, internal nodes..., leaves]
}

// NewTree builds a binary Merkle tree from the given leaves
// Requires: len(leaves) must be a power of 2
func NewTree(leaves [][]byte) *Tree {
	n := len(leaves)
	if n == 0 {
		panic("cannot create Merkle tree with 0 leaves")
	}
	if n&(n-1) != 0 {
		panic(fmt.Sprintf("number of leaves must be a power of 2, got %d", n))
	}

	// Build tree bottom-up
	nodes := make([][]byte, 2*n-1)

	// Copy leaves to the end of the nodes array
	for i := 0; i < n; i++ {
		nodes[n-1+i] = leaves[i]
	}

	// Build internal nodes from position n-2 (last internal) down to 0 (root)
	// n-1 internal nodes occupy positions [0, n-2]
	for i := n - 2; i >= 0; i-- {
		left := nodes[2*i+1]
		right := nodes[2*i+2]
		nodes[i] = hashPair(left, right)
	}

	return &Tree{
		nodes: nodes,
	}
}

// numLeaves returns the number of leaves in the tree
func (t *Tree) numLeaves() int {
	// With 2n-1 total nodes, n = (total+1)/2
	return (len(t.nodes) + 1) / 2
}

// depth returns the depth of the tree (calculated from number of leaves)
func (t *Tree) depth() int {
	n := t.numLeaves()
	if n <= 1 {
		return 0
	}
	return bits.Len(uint(n - 1))
}

// Root returns the Merkle root
func (t *Tree) Root() [32]byte {
	var root [32]byte
	copy(root[:], t.nodes[0])
	return root
}

// GenerateProof generates a Merkle proof for the leaf at the given index
func (t *Tree) GenerateProof(index int) ([][]byte, error) {
	n := t.numLeaves()
	if index < 0 || index >= n {
		return nil, fmt.Errorf("index %d out of range [0, %d)", index, n)
	}

	proof := make([][]byte, 0, t.depth())
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

// GenerateSubtreeProof generates a proof from the first k leaves to the full tree root
// Returns the sibling subtree roots needed to compute from k-leaf subtree to full root
// Requires: k must be a power of 2 and k < numLeaves
func (t *Tree) GenerateSubtreeProof(k int) ([][]byte, error) {
	n := t.numLeaves()

	if k <= 0 || k >= n {
		return nil, fmt.Errorf("k must be in range (0, %d), got %d", n, k)
	}
	if k&(k-1) != 0 {
		return nil, fmt.Errorf("k must be a power of 2, got %d", k)
	}

	proof := [][]byte{}
	currentSize := k

	// Algorithm from spec: collect sibling subtree roots
	for currentSize < n {
		// The root of sibling subtree [currentSize, currentSize*2) is at position:
		// (n + currentSize - 2) / currentSize
		siblingPos := (n + currentSize - 2) / currentSize
		proof = append(proof, t.nodes[siblingPos])
		currentSize = currentSize * 2
	}

	return proof, nil
}

// hashPair computes SHA256(left || right)
func hashPair(left, right []byte) []byte {
	h := sha256.New()
	h.Write(left)
	h.Write(right)
	return h.Sum(nil)
}
