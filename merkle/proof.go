package merkle

import (
	"fmt"
)

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
			proof = append(proof, t.nodes[sibling][:])
		}

		// Move to parent
		pos = (pos - 1) / 2
	}

	return proof, nil
}

// GenerateLeftSubtreeProof generates a proof from the leftmost k leaves to the full tree root
// Returns the sibling subtree roots needed to compute from k-leaf left subtree to full root
// Requires: k must be a power of 2 and k < numLeaves
func (t *Tree) GenerateLeftSubtreeProof(k int) ([][]byte, error) {
	n := t.numLeaves()

	if k <= 1 || k >= n {
		return nil, fmt.Errorf("k must be in range (1, %d), got %d", n, k)
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
		proof = append(proof, t.nodes[siblingPos][:])
		currentSize *= 2
	}

	return proof, nil
}

// ComputeRootFromProof computes the Merkle root given a leaf and its proof
func ComputeRootFromProof(leaf []byte, index int, proof [][]byte) ([32]byte, error) {
	// Start with the hashed leaf (apply leaf prefix like in tree construction)
	var current [32]byte
	hashLeaf(leaf, &current)
	pos := index

	// Traverse up the tree using the proof
	for _, siblingBytes := range proof {
		var sibling [32]byte
		copy(sibling[:], siblingBytes)

		var next [32]byte
		if pos%2 == 0 {
			// Current is left child
			hashPair(&current, &sibling, &next)
		} else {
			// Current is right child
			hashPair(&sibling, &current, &next)
		}
		current = next
		pos /= 2
	}

	return current, nil
}

// ComputeRootFromLeftSubtreeProof computes the full tree root given a left subtree root and sibling roots
// The subtree is assumed to be the leftmost k leaves where k is a power of 2
func ComputeRootFromLeftSubtreeProof(leftSubtreeRoot [32]byte, siblingRoots [][]byte) [32]byte {
	current := leftSubtreeRoot

	// Process each sibling in the proof
	for _, siblingBytes := range siblingRoots {
		var sibling [32]byte
		copy(sibling[:], siblingBytes)

		var next [32]byte
		// At each level, our current subtree is on the left,
		// and the sibling is on the right
		hashPair(&current, &sibling, &next)
		current = next
	}

	return current
}
