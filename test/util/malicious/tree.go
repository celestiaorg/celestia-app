package malicious

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/pkg/wrapper"
	"github.com/celestiaorg/go-square/shares"
	"github.com/celestiaorg/go-square/square"
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
	return constructor{
		squareSize: squareSize,
		opts:       opts,
	}.NewTree
}

// NewTree creates a new rsmt2d.Tree using the malicious
// wrapper.ErasuredNamespacedMerkleTree with predefined square size and
// nmt.Options.
func (c constructor) NewTree(_ rsmt2d.Axis, axisIndex uint) rsmt2d.Tree {
	hasher := NewNmtHasher(appconsts.NewBaseHashFunc(), appconsts.NamespaceSize, true)
	copts := []nmt.Option{
		nmt.CustomHasher(hasher),
		nmt.NamespaceIDSize(appconsts.NamespaceSize),
		nmt.IgnoreMaxNamespace(true),
	}
	copts = append(copts, c.opts...)
	nmtTree := nmt.New(appconsts.NewBaseHashFunc(), copts...)
	maliciousTree := &BlindTree{nmtTree}
	newTree := wrapper.NewErasuredNamespacedMerkleTree(c.squareSize, axisIndex, copts...)
	newTree.SetTree(maliciousTree)
	return &newTree
}

func ExtendShares(s [][]byte) (*rsmt2d.ExtendedDataSquare, error) {
	// Check that the length of the square is a power of 2.
	if !shares.IsPowerOfTwo(len(s)) {
		return nil, fmt.Errorf("number of shares is not a power of 2: got %d", len(s))
	}
	squareSize := square.Size(len(s))

	// here we construct a tree
	// Note: uses the nmt wrapper to construct the tree.
	return rsmt2d.ComputeExtendedDataSquare(s, appconsts.DefaultCodec(), NewConstructor(uint64(squareSize)))
}
