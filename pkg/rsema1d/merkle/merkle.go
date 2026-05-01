package merkle

import (
	"crypto/sha256"
	"fmt"
	"math/bits"
	"runtime"
	"sync"
)

// Prefix bytes for differentiating leaf and internal nodes (matching CometBFT/Tendermint)
var (
	leafPrefix  = []byte{0}
	innerPrefix = []byte{1}
)

// Tree represents a binary Merkle tree
type Tree struct {
	nodes [][32]byte // using array instead of slice enables single contiguous memory allocation for the entire tree
}

// NewTree builds a binary Merkle tree from the given leaves
// Requires: len(leaves) must be a power of 2
func NewTree(leaves [][]byte) *Tree {
	return NewTreeWithWorkers(leaves, runtime.NumCPU())
}

// NewTreeWithWorkers builds a binary Merkle tree using specified number of workers
// Requires: len(leaves) must be a power of 2
func NewTreeWithWorkers(leaves [][]byte, workerCount int) *Tree {
	n := len(leaves)
	if n == 0 {
		panic("cannot create Merkle tree with 0 leaves")
	}
	if n&(n-1) != 0 {
		panic(fmt.Sprintf("number of leaves must be a power of 2, got %d", n))
	}

	// Build tree bottom-up
	nodes := make([][32]byte, 2*n-1)

	// Parallel hash leaves and copy to the end of the nodes array
	parallelizeHashing(n, workerCount, func(i int) {
		hashLeaf(leaves[i], &nodes[n-1+i])
	})

	// Build internal nodes level by level, bottom-up
	for levelSize := n / 2; levelSize > 0; levelSize /= 2 {
		levelStart := levelSize - 1
		parallelizeHashing(levelSize, workerCount, func(i int) {
			pos := levelStart + i
			left := &nodes[2*pos+1]
			right := &nodes[2*pos+2]
			hashPair(left, right, &nodes[pos])
		})
	}

	return &Tree{
		nodes: nodes,
	}
}

// parallelizeHashing runs the hash function in parallel for count items
func parallelizeHashing(count int, workerCount int, hashFunc func(i int)) {
	if count <= 64 || workerCount <= 1 { // Small trees or single worker: sequential is faster
		for i := range count {
			hashFunc(i)
		}
		return
	}

	workers := min(workerCount, count)
	chunk := (count + workers - 1) / workers

	var wg sync.WaitGroup
	wg.Add(workers)
	for w := range workers {
		start := w * chunk
		end := min(start+chunk, count)
		go func(start, end int) {
			defer wg.Done()
			for i := start; i < end; i++ {
				hashFunc(i)
			}
		}(start, end)
	}
	wg.Wait()
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
	return t.nodes[0]
}

// hashLeaf hashes a leaf node with the leaf prefix, writing result directly to dst
func hashLeaf(data []byte, dst *[32]byte) {
	h := sha256.New()
	h.Write(leafPrefix)
	h.Write(data)
	h.Sum(dst[:0])
}

// hashPair hashes two nodes with the inner prefix, writing result directly to dst
func hashPair(left, right *[32]byte, dst *[32]byte) {
	h := sha256.New()
	h.Write(innerPrefix)
	h.Write(left[:])
	h.Write(right[:])
	h.Sum(dst[:0])
}
