package wrapper

import (
	"fmt"
	"sync"

	"github.com/celestiaorg/go-square/v2/share"
	"github.com/celestiaorg/nmt"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/celestiaorg/rsmt2d"

	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
)

// fixedTreePool provides a fixed-size pool of erasuredNamespacedMerkleTree instances
type fixedTreePool struct {
	availableNMTs chan *ErasuredNamespacedMerkleTree
	opts          []nmt.Option
	squareSize    uint64
}

func newFixedTreePool(size int, squareSize uint64, opts []nmt.Option) *fixedTreePool {
	pool := &fixedTreePool{
		availableNMTs: make(chan *ErasuredNamespacedMerkleTree, size),
		opts:          opts,
		squareSize:    squareSize,
	}

	for i := 0; i < size; i++ {
		tree := NewErasuredNamespacedMerkleTree(squareSize, 0, opts...)
		treePtr := &tree
		treePtr.pool = pool
		// Pre-allocate a buffer for all share data (2 * squareSize * shareSize bytes total)
		shareSize := share.ShareSize
		treePtr.buffer = make([]byte, 2*squareSize*uint64(shareSize))
		pool.availableNMTs <- treePtr
	}

	return pool
}

func (p *fixedTreePool) acquire() *ErasuredNamespacedMerkleTree {
	return <-p.availableNMTs
}

func (p *fixedTreePool) release(tree *ErasuredNamespacedMerkleTree) {
	p.availableNMTs <- tree
}

type TreePoolProvider struct {
	mutex     sync.Mutex
	factories map[uint64]*TreePool
	poolSize  int
	opts      []nmt.Option
}

// NewTreePoolProvider creates a new TreePoolProvider with the given default pool size and options
func NewTreePoolProvider(poolSize int, opts ...nmt.Option) *TreePoolProvider {
	return &TreePoolProvider{
		factories: make(map[uint64]*TreePool),
		poolSize:  poolSize,
		opts:      opts,
	}
}

// GetTreePool returns a cached TreePool for the given square size, creating one if it doesn't exist
func (tfp *TreePoolProvider) GetTreePool(squareSize uint64) *TreePool {
	tfp.mutex.Lock()
	defer tfp.mutex.Unlock()

	if factory, exists := tfp.factories[squareSize]; exists {
		return factory
	}

	factory := NewTreeFactory(squareSize, tfp.poolSize, tfp.opts...)
	tfp.factories[squareSize] = factory
	return factory
}

// Clear removes all cached factories (useful for testing or cleanup)
func (tfp *TreePoolProvider) Clear() {
	tfp.mutex.Lock()
	defer tfp.mutex.Unlock()
	tfp.factories = make(map[uint64]*TreePool)
}

// Size returns the number of cached factories
func (tfp *TreePoolProvider) Size() int {
	tfp.mutex.Lock()
	defer tfp.mutex.Unlock()
	return len(tfp.factories)
}

// TreePool provides pool methods for creating tree constructors
type TreePool struct {
	squareSize uint64
	opts       []nmt.Option
	poolSize   int
	treePool   *fixedTreePool
}

func NewTreeFactory(squareSize uint64, poolSize int, opts ...nmt.Option) *TreePool {
	return &TreePool{
		squareSize: squareSize,
		opts:       opts,
		poolSize:   poolSize,
		treePool:   newFixedTreePool(poolSize, squareSize, opts),
	}
}

func (f *TreePool) PoolSize() int {
	return f.poolSize
}

// SquareSize returns the square size this pool was configured for
func (f *TreePool) SquareSize() uint64 {
	return f.squareSize
}

func (f *TreePool) NewConstructor() rsmt2d.TreeConstructorFn {
	return func(_ rsmt2d.Axis, axisIndex uint) rsmt2d.Tree {
		tree := f.treePool.acquire()
		tree.axisIndex = uint64(axisIndex)
		tree.shareIndex = 0

		// Reset the tree (but don't put leaves back to pool anymore)
		tree.tree.Reset()

		return tree
	}
}

var (
	_ rsmt2d.Tree = &ErasuredNamespacedMerkleTree{}
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
	pool       *fixedTreePool // reference to the pool this tree belongs to
	buffer     []byte         // Pre-allocated buffer for share data
}

// Tree is the interface that rsmt2d expects
type Tree interface {
	Root() ([]byte, error)
	FastRoot() ([]byte, error)
	Push(namespacedData namespace.PrefixedData) error
	ProveRange(start, end int) (nmt.Proof, error)
	Reset() [][]byte
}

// NewErasuredNamespacedMerkleTree creates a new ErasuredNamespacedMerkleTree
// with an underlying NMT of namespace size `share.NamespaceSize` and with
// `ignoreMaxNamespace=true`. axisIndex is the index of the row or column that
// this tree is committing to. squareSize must be greater than zero.
func NewErasuredNamespacedMerkleTree(squareSize uint64, axisIndex uint, options ...nmt.Option) ErasuredNamespacedMerkleTree {
	if squareSize == 0 {
		panic("cannot create an ErasuredNamespacedMerkleTree of squareSize == 0")
	}

	options = append(options, nmt.NamespaceIDSize(share.NamespaceSize))
	options = append(options, nmt.IgnoreMaxNamespace(true))
	return ErasuredNamespacedMerkleTree{
		squareSize: squareSize,
		options:    options,
		tree:       nmt.New(appconsts.NewBaseHashFunc(), options...),
		axisIndex:  uint64(axisIndex),
		shareIndex: 0,
	}
}

type constructor struct {
	squareSize uint64
	opts       []nmt.Option
	pool       *TreePool
}

// NewBufferedConstructor creates a tree constructor function as required by rsmt2d to
// calculate the data root. It creates that tree using the
// wrapper.ErasuredNamespacedMerkleTree. It pools the trees for reuse.
func NewBufferedConstructor(squareSize uint64, pool *TreePool, opts ...nmt.Option) rsmt2d.TreeConstructorFn {
	return constructor{
		squareSize: squareSize,
		opts:       opts,
		pool:       pool,
	}.newBufferedTree
}

// NewConstructor creates a tree constructor function as required by rsmt2d to
// calculate the data root. It creates that tree using the
// wrapper.ErasuredNamespacedMerkleTree.
func NewConstructor(squareSize uint64, opts ...nmt.Option) rsmt2d.TreeConstructorFn {
	return constructor{
		squareSize: squareSize,
		opts:       opts,
	}.newTree
}

// newTree creates a new rsmt2d.Tree using the
// wrapper.ErasuredNamespacedMerkleTree with predefined square size and
// nmt.Options
func (c constructor) newTree(axis rsmt2d.Axis, axisIndex uint) rsmt2d.Tree {
	tree := NewErasuredNamespacedMerkleTree(c.squareSize, axisIndex, c.opts...)
	return &tree
}

// newBufferedTree gets a new rsmt2d.Tree from pool
// with predefined square size and nmt.Options
func (c constructor) newBufferedTree(axis rsmt2d.Axis, axisIndex uint) rsmt2d.Tree {
	// Use the pool's NewConstructor method to create a tree
	constructorFn := c.pool.NewConstructor()
	return constructorFn(axis, axisIndex)
}

// Push adds the provided data to the underlying NamespaceMerkleTree, and
// automatically uses the first share.NamespaceSize number of bytes as the
// namespace unless the data is pushed to the second half of the tree. Fulfills the
// rsmt2d.Tree interface. NOTE: returns an error if encountered while pushing or
// if the tree size is exceeded.
func (w *ErasuredNamespacedMerkleTree) Push(data []byte) error {
	if w.axisIndex+1 > 2*w.squareSize || w.shareIndex+1 > 2*w.squareSize {
		return fmt.Errorf("pushed past predetermined square size: boundary at %d index at %d %d", 2*w.squareSize, w.axisIndex, w.shareIndex)
	}
	if len(data) < share.NamespaceSize {
		return fmt.Errorf("data is too short to contain namespace ID")
	}

	var nidAndData []byte
	if w.buffer != nil {
		offset := int(w.shareIndex) * share.ShareSize
		nidAndData = w.buffer[offset : offset+len(data)]
	} else {
		nidAndData = make([]byte, len(data))
	}
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

func (w *ErasuredNamespacedMerkleTree) Release() {
	if w.pool != nil {
		w.axisIndex = 0
		w.shareIndex = 0

		w.pool.release(w)
	}
}

// FastRoot returns the Merkle root by consuming internal tree state for faster computation
func (w *ErasuredNamespacedMerkleTree) FastRoot() ([]byte, error) {
	return w.tree.FastRoot()
}

// Root fulfills the rsmt2d.Tree interface by generating and returning the
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
