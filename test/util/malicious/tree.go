package malicious

import (
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/wrapper"
	"github.com/celestiaorg/nmt"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/celestiaorg/rsmt2d"
)

// BlindTree is a wrapper around the erasured NMT that skips verification of
// namespace ordering when hashing and adding leaves to the tree. It does this
// by using a custom malicious hahser and wraps using the ForceAddLeaf method
// instead of the normal Push.
type BlindTree struct {
	*nmt.NamespacedMerkleTree
}

// Push overwrites the nmt method to skip namespace verification.
func (bt *BlindTree) Push(data namespace.PrefixedData) error {
	return bt.ForceAddLeaf(data)
}

type constructor struct {
	squareSize uint64
	opts       []nmt.Option
}

// NewConstructor creates a tree constructor function as required by rsmt2d to
// calculate the data root. It creates that tree using a malicious version of
// the wrapper.ErasuredNamespacedMerkleTree.
func NewConstructor(squareSize uint64, opts ...nmt.Option) rsmt2d.TreeConstructorFn {
	hasher := NewBlindHasher(appconsts.NewBaseHashFunc(), appconsts.NamespaceSize, true)
	copts := []nmt.Option{
		nmt.CustomHasher(hasher),
		nmt.NamespaceIDSize(appconsts.NamespaceSize),
		nmt.IgnoreMaxNamespace(true),
	}
	opts = append(opts, copts...)
	return constructor{
		squareSize: squareSize,
		opts:       opts,
	}.NewTree
}

// NewTree creates a new rsmt2d.Tree using the malicious
// wrapper.ErasuredNamespacedMerkleTree with predefined square size and
// nmt.Options.
func (c constructor) NewTree(_ rsmt2d.Axis, axisIndex uint) rsmt2d.Tree {
	nmtTree := nmt.New(appconsts.NewBaseHashFunc(), c.opts...)
	maliciousTree := &BlindTree{nmtTree}
	newTree := wrapper.NewErasuredNamespacedMerkleTree(c.squareSize, axisIndex, c.opts...)
	newTree.SetTree(maliciousTree)
	return &newTree
}
