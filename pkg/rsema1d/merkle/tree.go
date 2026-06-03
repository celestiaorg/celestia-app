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

// NewTree builds a tree from materialized leaves, allocating node storage.
// len(leaves) must be a positive power of 2.
func NewTree(leaves [][]byte, workerCount int) *Tree {
	return NewTreeInto(make([]byte, TreeBufferSize(len(leaves))), leaves, workerCount)
}

// NewTreeInto builds a tree from materialized leaves into caller-provided storage
// instead of allocating. buf is the flat node buffer; its length must be exactly
// [TreeBufferSize](n) bytes for a power-of-2 leaf count n, which the length encodes
// and len(leaves) must match. The returned Tree aliases buf, which must outlive it.
func NewTreeInto(buf []byte, leaves [][]byte, workerCount int) *Tree {
	n := leavesFromBuf(buf)
	if n != len(leaves) {
		panic(fmt.Sprintf("Merkle node buffer sized for %d leaves, got %d", n, len(leaves)))
	}
	t := &Tree{nodes: buf}

	parallelize(n, workerCount, func(i int) {
		hashLeaf(leaves[i], t.node(n-1+i))
	})
	t.hashNodes(n, workerCount)
	return t
}

// NewTreeFuncInto is [NewTreeInto] for leaves produced on demand: it takes a
// callback instead of a materialized slice, so buf alone determines the leaf count.
//
// leaf(i, dst) returns leaf i's bytes. dst is the slice leaf last returned — nil on
// the first call, and per worker in parallel builds — offered for reuse: a leaf
// that serializes into scratch grows dst when cap is too small and returns it; one
// that already holds its bytes ignores dst and returns them directly.
func NewTreeFuncInto(buf []byte, workerCount int, leaf func(i int, dst []byte) []byte) *Tree {
	n := leavesFromBuf(buf)
	t := &Tree{nodes: buf}

	hashLeaves(n, workerCount, leaf, func(i int, b []byte) {
		hashLeaf(b, t.node(n-1+i))
	})
	t.hashNodes(n, workerCount)
	return t
}

// TreeBufferSize returns the byte length of the node buffer a tree of the given
// leaf count needs, as accepted by [NewTreeInto] and [NewTreeFuncInto].
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

// RootFromFunc computes a Merkle root without retaining proof nodes. buf is
// the flat working level of one node per leaf: len(buf)/[NodeSize] is the leaf
// count and must be a power of 2. leaf(i, dst) returns the bytes for leaf i,
// recycling dst as in [NewTreeFuncInto]; this is always sequential, so one dst
// threads through every leaf.
func RootFromFunc(buf []byte, leaf func(i int, dst []byte) []byte) Root {
	leaves := nodeCount(buf)
	requirePow2(leaves)

	node := func(i int) []byte { return buf[i*NodeSize : (i+1)*NodeSize] }
	var dst []byte
	for i := range leaves {
		s := leaf(i, dst)
		hashLeaf(s, node(i))
		dst = s
	}
	for levelSize := leaves; levelSize > 1; levelSize /= 2 {
		for i := range levelSize / 2 {
			hashPair(node(2*i), node(2*i+1), node(i))
		}
	}

	var r Root
	copy(r[:], node(0))
	return r
}

// leavesFromBuf derives and validates the leaf count of a full tree from its
// flat node buffer: len(buf) must be exactly [TreeBufferSize](leaves) —
// NodeSize*(2*leaves-1) bytes — for a power-of-2 leaf count.
func leavesFromBuf(buf []byte) int {
	nodes := nodeCount(buf)
	leaves := (nodes + 1) / 2
	if treeNodeCount(leaves) != nodes {
		panic(fmt.Sprintf("Merkle node buffer holds %d nodes, not a full binary tree", nodes))
	}
	requirePow2(leaves)
	return leaves
}

// nodeCount returns the number of [NodeSize]-byte nodes in buf, validating
// that its length is a non-zero multiple of NodeSize.
func nodeCount(buf []byte) int {
	if len(buf) == 0 || len(buf)%NodeSize != 0 {
		panic(fmt.Sprintf("Merkle node buffer must be a non-zero multiple of %d bytes, got %d", NodeSize, len(buf)))
	}
	return len(buf) / NodeSize
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
