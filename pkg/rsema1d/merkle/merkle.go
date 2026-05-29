package merkle

import (
	"crypto/sha256"
	"fmt"
	"math/bits"
	"sync"
)

// Prefix bytes for differentiating leaf and internal nodes (matching CometBFT/Tendermint)
var (
	leafPrefix  = []byte{0}
	innerPrefix = []byte{1}
)

// Root is the canonical type for a 32-byte SHA-256 Merkle root.
type Root = [32]byte

// Tree represents a binary Merkle tree
type Tree struct {
	nodes [][32]byte // using array instead of slice enables single contiguous memory allocation for the entire tree
}

// NewTree builds a binary Merkle tree from materialized leaves.
// Requires: len(leaves) must be a positive power of 2.
func NewTree(leaves [][]byte, workerCount int) *Tree {
	n, nodes := prepareTreeStorageFromCount(len(leaves), make([][32]byte, treeNodeCount(len(leaves))))

	parallelizeHashing(n, workerCount, func(i int) {
		hashLeaf(leaves[i], &nodes[n-1+i])
	})
	buildInternalNodes(nodes, n, workerCount)

	return &Tree{nodes: nodes}
}

// NewTreeFromWriter builds a binary Merkle tree by writing each leaf into
// worker-local scratch before hashing it.
// Requires: leaves must be a positive power of 2.
func NewTreeFromWriter(leaves int, leafSize int, workerCount int, writeLeaf func(i int, dst []byte)) *Tree {
	n, nodes := prepareTreeStorageFromCount(leaves, make([][32]byte, treeNodeCount(leaves)))

	hashLeavesWithWriter(n, leafSize, workerCount, writeLeaf, func(i int, leaf []byte) {
		hashLeaf(leaf, &nodes[n-1+i])
	})
	buildInternalNodes(nodes, n, workerCount)

	return &Tree{nodes: nodes}
}

func treeNodeCount(leafCount int) int {
	if leafCount == 0 {
		return 0
	}
	return 2*leafCount - 1
}

func buildInternalNodes(nodes [][32]byte, n int, workerCount int) {
	for levelSize := n / 2; levelSize > 0; levelSize /= 2 {
		levelStart := levelSize - 1
		parallelizeHashing(levelSize, workerCount, func(i int) {
			pos := levelStart + i
			hashPair(&nodes[2*pos+1], &nodes[2*pos+2], &nodes[pos])
		})
	}
}

func prepareTreeStorageFromCount(n int, nodes [][32]byte) (int, [][32]byte) {
	if n == 0 {
		panic("cannot create Merkle tree with 0 leaves")
	}
	if n&(n-1) != 0 {
		panic(fmt.Sprintf("number of leaves must be a power of 2, got %d", n))
	}
	if len(nodes) < treeNodeCount(n) {
		panic(fmt.Sprintf("Merkle tree storage too small: need %d nodes, got %d", treeNodeCount(n), len(nodes)))
	}
	return n, nodes[:treeNodeCount(n)]
}

func hashLeavesWithWriter(leaves int, leafSize int, workerCount int, writeLeaf func(i int, dst []byte), hash func(i int, leaf []byte)) {
	if leaves <= 64 || workerCount <= 1 {
		leafScratch := make([]byte, leafSize)
		for i := range leaves {
			clear(leafScratch)
			writeLeaf(i, leafScratch)
			hash(i, leafScratch)
		}
		return
	}

	workers := min(workerCount, leaves)
	chunk := (leaves + workers - 1) / workers

	var wg sync.WaitGroup
	wg.Add(workers)
	for w := range workers {
		start := w * chunk
		end := min(start+chunk, leaves)
		go func(start, end int) {
			defer wg.Done()
			leafScratch := make([]byte, leafSize)
			for i := start; i < end; i++ {
				clear(leafScratch)
				writeLeaf(i, leafScratch)
				hash(i, leafScratch)
			}
		}(start, end)
	}
	wg.Wait()
}

// parallelizeHashing runs the hash function in parallel for count items.
func parallelizeHashing(count int, workerCount int, hashFunc func(i int)) {
	if count <= 64 || workerCount <= 1 {
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

// ComputeRootFromWriter computes a Merkle root without retaining proof nodes.
// scratch must have length at least leaves. leafScratch is cleared and passed
// to writeLeaf for each index, then hashed as a Merkle leaf internally.
// Requires: leaves must be a positive power of 2.
func ComputeRootFromWriter(scratch [][32]byte, leafScratch []byte, leaves int, writeLeaf func(i int, dst []byte)) Root {
	if leaves == 0 {
		panic("cannot compute Merkle root with 0 leaves")
	}
	if leaves&(leaves-1) != 0 {
		panic(fmt.Sprintf("number of leaves must be a power of 2, got %d", leaves))
	}
	if len(scratch) < leaves {
		panic(fmt.Sprintf("Merkle root scratch too small: need %d nodes, got %d", leaves, len(scratch)))
	}

	level := scratch[:leaves]
	for i := range leaves {
		clear(leafScratch)
		writeLeaf(i, leafScratch)
		hashLeaf(leafScratch, &level[i])
	}
	for levelSize := leaves; levelSize > 1; levelSize /= 2 {
		for i := range levelSize / 2 {
			hashPair(&level[2*i], &level[2*i+1], &level[i])
		}
	}
	return level[0]
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
func (t *Tree) Root() Root {
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
