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
	nodes [][]byte // All nodes: [root, internal nodes..., leaves]
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
	nodes := make([][]byte, 2*n-1)

	// Parallel hash leaves and copy to the end of the nodes array
	parallelizeHashing(n, workerCount, func(i int) {
		nodes[n-1+i] = hashLeaf(leaves[i])
	})

	// Build internal nodes level by level, bottom-up
	for levelSize := n / 2; levelSize > 0; levelSize /= 2 {
		levelStart := levelSize - 1
		parallelizeHashing(levelSize, workerCount, func(i int) {
			pos := levelStart + i
			left := nodes[2*pos+1]
			right := nodes[2*pos+2]
			nodes[pos] = hashPair(left, right)
		})
	}

	return &Tree{
		nodes: nodes,
	}
}

// parallelizeHashing runs the hash function in parallel for count items
func parallelizeHashing(count int, workerCount int, hashFunc func(i int)) {
	if count <= 64 || workerCount <= 1 { // Small trees or single worker: sequential is faster
		for i := 0; i < count; i++ {
			hashFunc(i)
		}
		return
	}

	// Use worker pool pattern for larger trees
	workers := workerCount
	if workers > count {
		workers = count
	}

	var wg sync.WaitGroup
	ch := make(chan int, count)

	// Start workers
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for i := range ch {
				hashFunc(i)
			}
		}()
	}

	// Send work
	for i := 0; i < count; i++ {
		ch <- i
	}
	close(ch)

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
	var root [32]byte
	copy(root[:], t.nodes[0])
	return root
}

// hashLeaf hashes a leaf node with the leaf prefix
func hashLeaf(data []byte) []byte {
	h := sha256.New()
	h.Write(leafPrefix)
	h.Write(data)
	return h.Sum(nil)
}

// hashPair hashes two nodes with the inner prefix
func hashPair(left, right []byte) []byte {
	h := sha256.New()
	h.Write(innerPrefix)
	h.Write(left)
	h.Write(right)
	return h.Sum(nil)
}
