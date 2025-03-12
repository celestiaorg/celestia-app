package wrapper

import (
	"fmt"

	"github.com/celestiaorg/go-square/v2/share"
	"github.com/celestiaorg/nmt"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/celestiaorg/rsmt2d"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
)

// Fulfills the rsmt2d.Tree interface and rsmt2d.TreeConstructorFn function
var (
	_ rsmt2d.TreeConstructorFn = NewConstructor(0)
	_ rsmt2d.Tree              = &ErasuredNamespacedMerkleTree{}
)

// ErasuredNamespacedMerkleTree wraps NamespaceMerkleTree to conform to the
// rsmt2d.Tree interface while also providing the correct namespaces to the
// underlying NamespaceMerkleTree. It does this by adding the already included
// namespace to the first half of the tree, and then uses the parity namespace
// ID for each share pushed to the second half of the tree. This allows for the
// namespaces to be included in the erasure data, while also keeping the nmt
// library sufficiently general
type ErasuredNamespacedMerkleTree struct {
	squareSize uint64 // note: this refers to the width of the original square before erasure-coded
	options    []nmt.Option
	tree       Tree
	// axisIndex is the index of the axis (row or column) that this tree is on. This is passed
	// by rsmt2d and used to help determine which quadrant each leaf belongs to.
	axisIndex uint64
	// shareIndex is the index of the share in a row or column that is being
	// pushed to the tree. It is expected to be in the range: 0 <= shareIndex <
	// 2*squareSize. shareIndex is used to help determine which quadrant each
	// leaf belongs to, along with keeping track of how many leaves have been
	// added to the tree so far.
	shareIndex uint64
}

// Tree is an interface that wraps the methods of the underlying
// NamespaceMerkleTree that are used by ErasuredNamespacedMerkleTree. This
// interface is mainly used for testing. It is not recommended to use this
// interface by implementing a different implementation.
type Tree interface {
	Root() ([]byte, error)
	Push(namespacedData namespace.PrefixedData) error
	ProveRange(start, end int) (nmt.Proof, error)
}

// NewErasuredNamespacedMerkleTree creates a new ErasuredNamespacedMerkleTree
// with an underlying NMT of namespace size `appconsts.NamespaceSize` and with
// `ignoreMaxNamespace=true`. axisIndex is the index of the row or column that
// this tree is committing to. squareSize must be greater than zero.
func NewErasuredNamespacedMerkleTree(squareSize uint64, axisIndex uint, options ...nmt.Option) ErasuredNamespacedMerkleTree {
	if squareSize == 0 {
		panic("cannot create an ErasuredNamespacedMerkleTree of squareSize == 0")
	}
	options = append(options, nmt.NamespaceIDSize(share.NamespaceSize))
	options = append(options, nmt.IgnoreMaxNamespace(true))
	tree := nmt.New(appconsts.NewBaseHashFunc(), options...)
	return ErasuredNamespacedMerkleTree{squareSize: squareSize, options: options, tree: tree, axisIndex: uint64(axisIndex), shareIndex: 0}
}

type constructor struct {
	squareSize uint64
	opts       []nmt.Option
}

// NewConstructor creates a tree constructor function as required by rsmt2d to
// calculate the data root. It creates that tree using the
// wrapper.ErasuredNamespacedMerkleTree.
func NewConstructor(squareSize uint64, opts ...nmt.Option) rsmt2d.TreeConstructorFn {
	return constructor{
		squareSize: squareSize,
		opts:       opts,
	}.NewTree
}

// NewTree creates a new rsmt2d.Tree using the
// wrapper.ErasuredNamespacedMerkleTree with predefined square size and
// nmt.Options
func (c constructor) NewTree(_ rsmt2d.Axis, axisIndex uint) rsmt2d.Tree {
	newTree := NewErasuredNamespacedMerkleTree(c.squareSize, axisIndex, c.opts...)
	return &newTree
}

// Push adds the provided data to the underlying NamespaceMerkleTree, and
// automatically uses the first DefaultNamespaceIDLen number of bytes as the
// namespace unless the data pushed to the second half of the tree. Fulfills the
// rsmt.Tree interface. NOTE: panics if an error is encountered while pushing or
// if the tree size is exceeded.
func (w *ErasuredNamespacedMerkleTree) Push(data []byte) error {
	if w.axisIndex+1 > 2*w.squareSize || w.shareIndex+1 > 2*w.squareSize {
		return fmt.Errorf("pushed past predetermined square size: boundary at %d index at %d %d", 2*w.squareSize, w.axisIndex, w.shareIndex)
	}
	if len(data) < share.NamespaceSize {
		return fmt.Errorf("data is too short to contain namespace ID")
	}
	nidAndData := make([]byte, share.NamespaceSize+len(data))
	copy(nidAndData[share.NamespaceSize:], data)
	// use the parity namespace if the cell is not in Q0 of the extended data square
	if w.isQuadrantZero() {
		copy(nidAndData[:share.NamespaceSize], data[:share.NamespaceSize])
	} else {
		copy(nidAndData[:share.NamespaceSize], share.ParitySharesNamespace.Bytes())
	}
	err := w.tree.Push(nidAndData)
	if err != nil {
		return err
	}
	w.incrementShareIndex()
	return nil
}

// Root fulfills the rsmt.Tree interface by generating and returning the
// underlying NamespaceMerkleTree Root.
func (w *ErasuredNamespacedMerkleTree) Root() ([]byte, error) {
	root, err := w.tree.Root()
	if err != nil {
		return nil, err
	}
	return root, nil
}

// ProveRange returns a Merkle range proof for the leaf range [start, end] where `end` is non-inclusive.
func (w *ErasuredNamespacedMerkleTree) ProveRange(start, end int) (nmt.Proof, error) {
	return w.tree.ProveRange(start, end)
}

// incrementShareIndex increments the share index by one.
func (w *ErasuredNamespacedMerkleTree) incrementShareIndex() {
	w.shareIndex++
}

// isQuadrantZero returns true if the current share index and axis index are both
// in the original data square.
func (w *ErasuredNamespacedMerkleTree) isQuadrantZero() bool {
	return w.shareIndex < w.squareSize && w.axisIndex < w.squareSize
}

// SetTree sets the underlying tree to the provided tree. This is used for
// testing purposes only.
func (w *ErasuredNamespacedMerkleTree) SetTree(tree Tree) {
	w.tree = tree
}
