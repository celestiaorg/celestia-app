package merkle

import "fmt"

// Proof returns the Merkle proof (sibling hashes, leaf to root) for the
// leaf at index.
func (t *Tree) Proof(index int) ([][]byte, error) {
	proof := make([][]byte, t.depth())
	if err := t.fillProof(proof, index); err != nil {
		return nil, err
	}
	return proof, nil
}

// Proofs yields the proof for the leaf at each of positions, carving all
// proofs from a single arena. yield(i, proof) gets the proof for positions[i];
// each proof aliases tree storage and is valid for the tree's lifetime. Stops on
// the first out-of-range position.
func (t *Tree) Proofs(positions []int, yield func(i int, proof [][]byte)) error {
	depth := t.depth()
	arena := make([][]byte, len(positions)*depth)
	for i, pos := range positions {
		proof := arena[i*depth : (i+1)*depth]
		if err := t.fillProof(proof, pos); err != nil {
			return err
		}
		yield(i, proof)
	}
	return nil
}

// fillProof writes index's sibling hashes into dst, one per level. dst must have
// len == depth; the full power-of-2 tree fills every entry. Elements alias tree
// storage.
func (t *Tree) fillProof(dst [][]byte, index int) error {
	n := t.numLeaves()
	if index < 0 || index >= n {
		return fmt.Errorf("index %d out of range [0, %d)", index, n)
	}

	// Traverse from leaf to root, recording each level's sibling.
	pos := n - 1 + index
	for level := 0; pos > 0; level++ {
		sibling := pos - 1 // left sibling
		if pos%2 == 1 {
			sibling = pos + 1 // right sibling
		}
		dst[level] = t.node(sibling)
		pos = (pos - 1) / 2
	}

	return nil
}

// RootFromProof recomputes the Merkle root from a leaf and its proof.
func RootFromProof(leaf []byte, index int, proof [][]byte) (Root, error) {
	var current Root
	hashLeaf(leaf, current[:])

	pos := index
	for _, siblingBytes := range proof {
		if len(siblingBytes) != NodeSize {
			return Root{}, fmt.Errorf("proof sibling must be %d bytes, got %d", NodeSize, len(siblingBytes))
		}
		var sibling Root
		copy(sibling[:], siblingBytes)

		var next Root
		if pos%2 == 0 {
			hashPair(current[:], sibling[:], next[:]) // current is the left child
		} else {
			hashPair(sibling[:], current[:], next[:]) // current is the right child
		}
		current = next
		pos /= 2
	}

	return current, nil
}

// ProofInput bundles the inputs [RootFromProof] needs for a single leaf.
// It is the per-proof payload for [RootFromProofs].
type ProofInput struct {
	Leaf  []byte
	Index int
	Path  [][]byte
}

// proofGrain is the per-worker proof floor for [RootFromProofs]. A proof costs
// about tree-depth hashes — far more than one node — so a batch parallelizes at a
// much smaller size than raw hashing (hashGrain).
const proofGrain = 32

// RootFromProofs verifies a batch of Merkle proofs and returns the single root
// they all share, erroring if any proof yields a different root (at least one was
// tampered). Intended for callers that already know all proofs verify the same
// tree, e.g. every row of a shard. Fans out across up to workers goroutines.
func RootFromProofs(inputs []ProofInput, workers int) (Root, error) {
	if len(inputs) == 0 {
		return Root{}, fmt.Errorf("no proof inputs")
	}
	nw := splitWorkers(len(inputs), workers, proofGrain)
	if nw <= 1 {
		return reduceProofs(inputs, 0, len(inputs))
	}

	roots := make([]Root, nw)
	errs := make([]error, nw)
	n := parallelChunks(len(inputs), nw, func(w, start, end int) {
		roots[w], errs[w] = reduceProofs(inputs, start, end)
	})

	for w := range n {
		if errs[w] != nil {
			return Root{}, errs[w]
		}
		if roots[w] != roots[0] {
			return Root{}, fmt.Errorf("proof chunk %d: root mismatch", w)
		}
	}
	return roots[0], nil
}

// reduceProofs recomputes the root of inputs[start:end] (a non-empty range) and
// returns it, erroring if any input yields a different root than the range's first.
func reduceProofs(inputs []ProofInput, start, end int) (Root, error) {
	want, err := RootFromProof(inputs[start].Leaf, inputs[start].Index, inputs[start].Path)
	if err != nil {
		return Root{}, fmt.Errorf("input %d (tree index %d): %w", start, inputs[start].Index, err)
	}
	for i := start + 1; i < end; i++ {
		root, err := RootFromProof(inputs[i].Leaf, inputs[i].Index, inputs[i].Path)
		if err != nil {
			return Root{}, fmt.Errorf("input %d (tree index %d): %w", i, inputs[i].Index, err)
		}
		if root != want {
			return Root{}, fmt.Errorf("input %d (tree index %d): root mismatch", i, inputs[i].Index)
		}
	}
	return want, nil
}
