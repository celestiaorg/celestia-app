package merkle

import (
	"crypto/sha256"
	"fmt"
	"math/bits"
)

// NodeSize is the byte length of a Merkle node (one SHA-256 hash). Flat node
// buffers passed to the API are multiples of it.
const NodeSize = sha256.Size

// Root is a 32-byte SHA-256 Merkle root.
type Root = [NodeSize]byte

// Tree is a binary Merkle tree over a power-of-2 number of leaves. Nodes are
// stored flat — node i occupies nodes[i*NodeSize:(i+1)*NodeSize] — so the
// whole tree is one contiguous buffer.
type Tree struct {
	nodes []byte
}

// NewTree builds a tree over leaves, fanning out across up to workerCount
// goroutines. len(leaves) must be a positive power of 2.
//
// buf is the tree's flat node storage: exactly [TreeBufferSize](len(leaves))
// bytes, or nil to allocate it. The returned Tree aliases buf, which must
// outlive it and the proofs it hands out.
func NewTree(buf []byte, leaves [][]byte, workerCount int) *Tree {
	n := len(leaves)
	requirePow2(n)
	if buf == nil {
		buf = make([]byte, TreeBufferSize(n))
	}
	if len(buf) != TreeBufferSize(n) {
		panic(fmt.Sprintf("Merkle node buffer for %d leaves must be %d bytes, got %d", n, TreeBufferSize(n), len(buf)))
	}
	t := &Tree{nodes: buf}

	if n == 1 {
		hashLeaf(leaves[0], t.node(0))
	} else {
		parallelize(n/2, workerCount, func(j int) {
			i := 2 * j
			hashLeafPair(leaves[i], leaves[i+1], t.node(n-1+i), t.node(n+i))
		})
	}
	t.hashNodes(n, workerCount)
	return t
}

// TreeBufferSize returns the byte length of the node buffer a tree of the given
// leaf count needs, as accepted by [NewTree].
func TreeBufferSize(leaves int) int {
	return treeNodeCount(leaves) * NodeSize
}

// Root returns the Merkle root.
func (t *Tree) Root() Root {
	var r Root
	copy(r[:], t.nodes[:NodeSize])
	return r
}

// node returns the i-th node's NodeSize-byte slice within the flat buffer.
func (t *Tree) node(i int) []byte {
	off := i * NodeSize
	return t.nodes[off : off+NodeSize]
}

// numLeaves returns the number of leaves: with 2n-1 total nodes, n = (total+1)/2.
func (t *Tree) numLeaves() int {
	return (len(t.nodes)/NodeSize + 1) / 2
}

// depth returns the number of levels between the leaves and the root.
func (t *Tree) depth() int {
	n := t.numLeaves()
	if n <= 1 {
		return 0
	}
	return bits.Len(uint(n - 1))
}

// hashNodes computes the internal nodes, hashing each level of sibling pairs up
// to the root. The per-level closure is hoisted out of the loop and reused — its
// levelStart is updated between levels, which is safe because parallelize joins
// all of a level's workers before the next iteration changes it — so the
// allocation count stays constant instead of growing with tree depth.
func (t *Tree) hashNodes(n, workerCount int) {
	var levelStart int
	hashLevel := func(i int) {
		pos := levelStart + i
		hashPair(t.node(2*pos+1), t.node(2*pos+2), t.node(pos))
	}
	for levelSize := n / 2; levelSize > 0; levelSize /= 2 {
		levelStart = levelSize - 1
		parallelize(levelSize, workerCount, hashLevel)
	}
}

func requirePow2(leaves int) {
	if leaves == 0 {
		panic("cannot build Merkle tree with 0 leaves")
	}
	if leaves&(leaves-1) != 0 {
		panic(fmt.Sprintf("number of leaves must be a power of 2, got %d", leaves))
	}
}

func treeNodeCount(leaves int) int {
	if leaves == 0 {
		return 0
	}
	return 2*leaves - 1
}
