package merkle

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// ProofInput bundles the inputs ComputeRootFromProof needs for a single
// leaf. Used as the per-proof payload for ComputeRootsFromProofs.
type ProofInput struct {
	Leaf  []byte
	Index int
	Path  [][]byte
}

// ComputeRootsFromProofs verifies a batch of merkle proofs in parallel and
// writes each computed root to dst[i]. dst must have at least len(inputs)
// capacity.
//
// The work fans out across up to workers goroutines via static index-range
// chunking; below the parallel break-even (len(inputs) <= 64 or workers <=
// 1) the call stays sequential — goroutine startup would otherwise dwarf
// per-proof SHA256 work. Returns the first error any worker observed.
func ComputeRootsFromProofs(inputs []ProofInput, dst [][32]byte, workers int) error {
	if len(dst) < len(inputs) {
		return fmt.Errorf("dst has length %d, need at least %d", len(dst), len(inputs))
	}
	verify := func(i int) error {
		root, err := ComputeRootFromProof(inputs[i].Leaf, inputs[i].Index, inputs[i].Path)
		if err != nil {
			return fmt.Errorf("input %d (tree index %d): %w", i, inputs[i].Index, err)
		}
		dst[i] = root
		return nil
	}

	if workers <= 1 || len(inputs) <= 64 {
		for i := range inputs {
			if err := verify(i); err != nil {
				return err
			}
		}
		return nil
	}
	if workers > len(inputs) {
		workers = len(inputs)
	}

	chunk := (len(inputs) + workers - 1) / workers
	var firstErr atomic.Value
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := range workers {
		start := w * chunk
		end := min(start+chunk, len(inputs))
		go func(start, end int) {
			defer wg.Done()
			for i := start; i < end; i++ {
				if err := verify(i); err != nil {
					firstErr.CompareAndSwap(nil, err)
					return
				}
			}
		}(start, end)
	}
	wg.Wait()
	if v := firstErr.Load(); v != nil {
		return v.(error)
	}
	return nil
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
