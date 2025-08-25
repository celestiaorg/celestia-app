package merkle

import (
	"crypto/sha256"
	"fmt"
	"math/bits"
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
	n := len(leaves)
	if n == 0 {
		panic("cannot create Merkle tree with 0 leaves")
	}
	if n&(n-1) != 0 {
		panic(fmt.Sprintf("number of leaves must be a power of 2, got %d", n))
	}

	// Build tree bottom-up
	nodes := make([][]byte, 2*n-1)

	// Hash leaves and copy to the end of the nodes array
	for i := 0; i < n; i++ {
		nodes[n-1+i] = hashLeaf(leaves[i])
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
