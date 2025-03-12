package inclusion

import (
	"fmt"
	"sync"

	"github.com/celestiaorg/nmt"
	"github.com/celestiaorg/rsmt2d"

	"github.com/celestiaorg/celestia-app/v4/pkg/da"
	"github.com/celestiaorg/celestia-app/v4/pkg/wrapper"
)

// WalkInstruction wraps the bool type to indicate the direction that should be
// used while traversing a binary tree
type WalkInstruction bool

const (
	WalkLeft  = false
	WalkRight = true
)

// subTreeRootCacher keep track of all the inner nodes of an nmt using a simple
// map. Note: this cacher does not cache individual leaves or their hashes, only
// inner nodes.
type subTreeRootCacher struct {
	cache map[string][2]string
}

func newSubTreeRootCacher() *subTreeRootCacher {
	return &subTreeRootCacher{cache: make(map[string][2]string)}
}

// Visit fulfills the nmt.NodeVisitorFn function definition. It stores each inner
// node in a simple map, which can later be used to walk the tree. This function
// is called by the nmt when calculating the root.
func (strc *subTreeRootCacher) Visit(hash []byte, children ...[]byte) {
	switch len(children) {
	case 2:
		strc.cache[string(hash)] = [2]string{string(children[0]), string(children[1])}
	case 1:
		return
	default:
		panic("unexpected visit")
	}
}

// walk recursively traverses the subTreeRootCacher's internal tree by using the
// provided sub tree root and path. The provided path should be a []bool, false
// indicating that the first child node (left most node) should be used to find
// the next path, and the true indicating that the second (right) should be used.
// walk throws an error if the sub tree cannot be found.
func (strc subTreeRootCacher) walk(root []byte, path []WalkInstruction) ([]byte, error) {
	// return if we've reached the end of the path
	if len(path) == 0 {
		return root, nil
	}
	// try to lookup the provided sub root
	children, has := strc.cache[string(root)]
	if !has {
		// note: we might want to consider panicking here
		return nil, fmt.Errorf("did not find sub tree root: %v", root)
	}

	// continue to traverse the tree by recursively calling this function on the next root
	switch path[0] {
	case WalkLeft:
		return strc.walk([]byte(children[0]), path[1:])
	case WalkRight:
		return strc.walk([]byte(children[1]), path[1:])
	default:
		// this is unreachable code, but the compiler doesn't recognize this somehow
		panic("bool other than true or false, computers were a mistake, everything is a lie, math is fake.")
	}
}

// EDSSubTreeRootCacher caches the inner nodes for each row so that we can
// traverse it later to check for blob inclusion. NOTE: Currently this is not
// threadsafe, but with a future refactor, we could simply read from rsmt2d and
// not use the tree constructor which would fix both of these issues.
type EDSSubTreeRootCacher struct {
	mut        *sync.RWMutex
	caches     map[uint]*subTreeRootCacher
	squareSize uint64
}

func NewSubtreeCacher(squareSize uint64) *EDSSubTreeRootCacher {
	return &EDSSubTreeRootCacher{
		mut:        &sync.RWMutex{},
		caches:     make(map[uint]*subTreeRootCacher),
		squareSize: squareSize,
	}
}

// Constructor fulfills the rsmt2d.TreeCreatorFn by keeping a pointer to the
// cache and embedding it as a nmt.NodeVisitor into a new wrapped nmt.
func (stc *EDSSubTreeRootCacher) Constructor(axis rsmt2d.Axis, axisIndex uint) rsmt2d.Tree {
	var newTree wrapper.ErasuredNamespacedMerkleTree
	switch axis {
	case rsmt2d.Row:
		strc := newSubTreeRootCacher()
		stc.mut.Lock()
		stc.caches[axisIndex] = strc
		stc.mut.Unlock()
		newTree = wrapper.NewErasuredNamespacedMerkleTree(stc.squareSize, axisIndex, nmt.NodeVisitor(strc.Visit))
	default:
		newTree = wrapper.NewErasuredNamespacedMerkleTree(stc.squareSize, axisIndex)
	}
	return &newTree
}

// getSubTreeRoot traverses the nmt of the selected row and returns the
// subtree root. An error is thrown if the subtree cannot be found.
func (stc *EDSSubTreeRootCacher) getSubTreeRoot(dah da.DataAvailabilityHeader, row int, path []WalkInstruction) ([]byte, error) {
	if len(stc.caches) != len(dah.RowRoots) {
		return nil, fmt.Errorf("data availability header has unexpected number of row roots: expected %d got %d", len(stc.caches), len(dah.RowRoots))
	}
	if row >= len(stc.caches) {
		return nil, fmt.Errorf("row exceeds range of cache: max %d got %d", len(stc.caches), row)
	}
	stc.mut.RLock()
	sbt, err := stc.caches[uint(row)].walk(dah.RowRoots[row], path)
	stc.mut.RUnlock()
	return sbt, err
}
