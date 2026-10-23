package rsmt2d

// The contents of this file have been adapted from the source file available at https://github.com/celestiaorg/celestia-app/blob/bab6c0d0befe677ab8c2f4b83561c08affc7203e/pkg/wrapper/nmt_wrapper.go,
// solely for the purpose of testing rsmt2d expected behavior when integrated with a NamespaceMerkleTree.
// Please note that this file has undergone several modifications and may not match the original file exactly.

import (
	"bytes"
	"crypto/sha256"
	"fmt"

	"github.com/celestiaorg/nmt"
	"github.com/celestiaorg/nmt/namespace"
)

// Fulfills the Tree interface and TreeConstructorFn function
var (
	_ Tree = &erasuredNamespacedMerkleTree{}
)

// erasuredNamespacedMerkleTree wraps NamespaceMerkleTree to conform to the
// Tree interface while also providing the correct namespaces to the
// underlying NamespaceMerkleTree. It does this by adding the already included
// namespace to the first half of the tree, and then uses the parity namespace
// ID for each share pushed to the second half of the tree. This allows for the
// namespaces to be included in the erasure data, while also keeping the nmt
// library sufficiently general
type erasuredNamespacedMerkleTree struct {
	squareSize uint64 // note: this refers to the width of the original square before erasure-coded
	options    []nmt.Option
	tree       nmtTree
	// axisIndex is the index of the axis (row or column) that this tree is on. This is passed
	// by rsmt2d and used to help determine which quadrant each leaf belongs to.
	axisIndex uint64
	// shareIndex is the index of the share in a row or column that is being
	// pushed to the tree. It is expected to be in the range: 0 <= shareIndex <
	// 2*squareSize. shareIndex is used to help determine which quadrant each
	// leaf belongs to, along with keeping track of how many leaves have been
	// added to the tree so far.
	shareIndex    uint64
	namespaceSize int
}

// nmtTree is an interface that wraps the methods of the underlying
// NamespaceMerkleTree that are used by erasuredNamespacedMerkleTree. This
// interface is mainly used for testing. It is not recommended to use this
// interface by implementing a different implementation.
type nmtTree interface {
	Root() ([]byte, error)
	Push(namespacedData namespace.PrefixedData) error
}

// newErasuredNamespacedMerkleTree creates a new erasuredNamespacedMerkleTree
// with an underlying NMT of namespace size `NamespaceSize` and with
// `ignoreMaxNamespace=true`. axisIndex is the index of the row or column that
// this tree is committing to. squareSize must be greater than zero.
func newErasuredNamespacedMerkleTree(squareSize uint64, axisIndex uint,
	options ...nmt.Option,
) erasuredNamespacedMerkleTree {
	if squareSize == 0 {
		panic("cannot create a erasuredNamespacedMerkleTree of squareSize == 0")
	}
	// read the options to extract the namespace size, and use it to construct erasuredNamespacedMerkleTree
	opts := &nmt.Options{}
	for _, setter := range options {
		setter(opts)
	}
	options = append(options, nmt.IgnoreMaxNamespace(true))
	tree := nmt.New(sha256.New(), options...)
	return erasuredNamespacedMerkleTree{squareSize: squareSize, namespaceSize: int(opts.NamespaceIDSize), options: options, tree: tree, axisIndex: uint64(axisIndex), shareIndex: 0}
}

type constructor struct {
	squareSize uint64
	opts       []nmt.Option
}

// newErasuredNamespacedMerkleTreeConstructor creates a tree constructor function as required by rsmt2d to
// calculate the data root. It creates that tree using the
// erasuredNamespacedMerkleTree.
func newErasuredNamespacedMerkleTreeConstructor(squareSize uint64, opts ...nmt.Option) TreeConstructorFn {
	return constructor{
		squareSize: squareSize,
		opts:       opts,
	}.NewTree
}

// NewTree creates a new Tree using the
// erasuredNamespacedMerkleTree with predefined square size and
// nmt.Options
func (c constructor) NewTree(_ Axis, axisIndex uint) Tree {
	newTree := newErasuredNamespacedMerkleTree(c.squareSize, axisIndex, c.opts...)
	return &newTree
}

// Push adds the provided data to the underlying NamespaceMerkleTree, and
// automatically uses the first erasuredNamespacedMerkleTree.namespaceSize number of bytes as the
// namespace unless the data pushed to the second half of the tree. Fulfills the
// rsmt2d.Tree interface. NOTE: panics if an error is encountered while pushing or
// if the tree size is exceeded.
func (w *erasuredNamespacedMerkleTree) Push(data []byte) error {
	ParitySharesNamespaceBytes := bytes.Repeat([]byte{0xFF}, w.namespaceSize)
	if w.axisIndex+1 > 2*w.squareSize || w.shareIndex+1 > 2*w.squareSize {
		return fmt.Errorf("pushed past predetermined square size: boundary at %d index at %d %d", 2*w.squareSize, w.axisIndex, w.shareIndex)
	}
	if len(data) < w.namespaceSize {
		return fmt.Errorf("data is too short to contain namespace ID")
	}
	nidAndData := make([]byte, w.namespaceSize+len(data))
	copy(nidAndData[w.namespaceSize:], data)
	// use the parity namespace if the cell is not in Q0 of the extended data square
	if w.isQuadrantZero() {
		copy(nidAndData[:w.namespaceSize], data[:w.namespaceSize])
	} else {
		copy(nidAndData[:w.namespaceSize], ParitySharesNamespaceBytes)
	}
	err := w.tree.Push(nidAndData)
	if err != nil {
		return err
	}
	w.incrementShareIndex()
	return nil
}

// Root fulfills the rsmt2d.Tree interface by generating and returning the
// underlying NamespaceMerkleTree Root.
func (w *erasuredNamespacedMerkleTree) Root() ([]byte, error) {
	root, err := w.tree.Root()
	if err != nil {
		return nil, err
	}
	return root, nil
}

// incrementShareIndex increments the share index by one.
func (w *erasuredNamespacedMerkleTree) incrementShareIndex() {
	w.shareIndex++
}

// isQuadrantZero returns true if the current share index and axis index are both
// in the original data square.
func (w *erasuredNamespacedMerkleTree) isQuadrantZero() bool {
	return w.shareIndex < w.squareSize && w.axisIndex < w.squareSize
}
