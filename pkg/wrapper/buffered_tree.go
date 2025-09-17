package wrapper

import (
	"fmt"
	"runtime"
	"sync"

	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/go-square/v3/share"
	"github.com/celestiaorg/nmt"
	"github.com/celestiaorg/rsmt2d"
)

// fixedTreePool provides a fixed-size pool of bufferedTree instances
type fixedTreePool struct {
	availableNMTs chan *bufferedTree
	opts          []nmt.Option
	squareSize    uint64
}

// newFixedTreePool creates a fixed-size pool of bufferedTree instances
func newFixedTreePool(size int, squareSize uint64, opts []nmt.Option) *fixedTreePool {
	pool := &fixedTreePool{
		availableNMTs: make(chan *bufferedTree, size),
		opts:          opts,
		squareSize:    squareSize,
	}

	for i := 0; i < size; i++ {
		pool.availableNMTs <- newBufferedTree(squareSize, 0, pool, opts...)
	}

	return pool
}

// acquire retrieves a bufferedTree from the pool
func (p *fixedTreePool) acquire() *bufferedTree {
	return <-p.availableNMTs
}

// release returns a bufferedTree to the pool for reuse
func (p *fixedTreePool) release(tree *bufferedTree) {
	p.availableNMTs <- tree
}

type TreePoolProvider struct {
	mutex    sync.Mutex
	pools    map[uint64]*TreePool
	poolSize int
	opts     []nmt.Option
}

// NewTreePoolProvider creates a new TreePoolProvider with the given default pool size and options
func NewTreePoolProvider() *TreePoolProvider {
	return &TreePoolProvider{
		pools:    make(map[uint64]*TreePool),
		poolSize: runtime.NumCPU() * 4,
	}
}

// PreallocatePool creates a pool for the given square size and allocates buffers
func (p *TreePoolProvider) PreallocatePool(squareSize uint64) {
	_ = p.GetTreePool(squareSize)
}

// GetTreePool returns a cached TreePool for the given square size, creating one if it doesn't exist
func (p *TreePoolProvider) GetTreePool(squareSize uint64) *TreePool {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if pool, exists := p.pools[squareSize]; exists {
		return pool
	}

	pool := NewTreePool(squareSize, p.poolSize, p.opts...)
	p.pools[squareSize] = pool
	return pool
}

// TreePool provides pool methods for creating tree constructors
type TreePool struct {
	squareSize uint64
	opts       []nmt.Option
	poolSize   int
	treePool   *fixedTreePool
}

// NewTreePool creates a new TreePool with the specified configuration
func NewTreePool(squareSize uint64, poolSize int, opts ...nmt.Option) *TreePool {
	return &TreePool{
		squareSize: squareSize,
		opts:       opts,
		poolSize:   poolSize,
		treePool:   newFixedTreePool(poolSize, squareSize, opts),
	}
}

// BufferSize returns the number of trees in the pool
func (f *TreePool) BufferSize() int {
	return f.poolSize
}

// SquareSize returns the square size this pool was configured for
func (f *TreePool) SquareSize() uint64 {
	return f.squareSize
}

// NewConstructor returns a tree constructor function that uses the pool
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

var _ rsmt2d.Tree = &bufferedTree{}

// bufferedTree is a wrapper around NamespaceMerkleTree with buffer pooling support
// for efficient memory management in high-throughput scenarios.
type bufferedTree struct {
	squareSize uint64 // note: this refers to the width of the original square before erasure-coded
	options    []nmt.Option
	tree       *nmt.NamespacedMerkleTree
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

// newBufferedTree creates a new bufferedTree with pre-allocated buffer and pool reference
func newBufferedTree(squareSize uint64, axisIndex uint, pool *fixedTreePool, options ...nmt.Option) *bufferedTree {
	if squareSize == 0 {
		panic("cannot create a bufferedTree of squareSize == 0")
	}
	if pool == nil {
		panic("cannot create a bufferedTree of pool == nil")
	}
	options = append(options, nmt.NamespaceIDSize(share.NamespaceSize))
	options = append(options, nmt.IgnoreMaxNamespace(true))
	tree := nmt.New(appconsts.NewBaseHashFunc(), options...)
	entrySize := share.ShareSize + share.NamespaceSize
	return &bufferedTree{
		squareSize: squareSize,
		options:    options,
		tree:       tree,
		pool:       pool,
		axisIndex:  uint64(axisIndex),
		buffer:     make([]byte, 2*squareSize*uint64(entrySize)),
		shareIndex: 0,
	}
}

// Push adds share data to the tree using the pre-allocated buffer to avoid memory allocation
func (w *bufferedTree) Push(data []byte) error {
	if w.axisIndex+1 > 2*w.squareSize || w.shareIndex+1 > 2*w.squareSize {
		return fmt.Errorf("pushed past predetermined square size: boundary at %d index at %d %d", 2*w.squareSize, w.axisIndex, w.shareIndex)
	}
	if len(data) < share.NamespaceSize {
		return fmt.Errorf("data is too short to contain namespace ID")
	}
	var (
		nidAndData      []byte
		bufferEntrySize = share.NamespaceSize + share.ShareSize
		nidDataLen      = share.NamespaceSize + len(data)
	)

	if nidDataLen > bufferEntrySize {
		return fmt.Errorf("data is too large to be used with allocated buffer")
	}
	offset := int(w.shareIndex) * bufferEntrySize
	nidAndData = w.buffer[offset : offset+nidDataLen]

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

// Root calculates the tree root using FastRoot and releases the tree back to the pool
func (w *bufferedTree) Root() ([]byte, error) {
	defer w.pool.release(w)
	return w.tree.FastRoot()
}

// incrementShareIndex advances to the next share position in the tree
func (w *bufferedTree) incrementShareIndex() {
	w.shareIndex++
}

// isQuadrantZero checks if the current position is in the original (non-parity) data quadrant
func (w *bufferedTree) isQuadrantZero() bool {
	return w.shareIndex < w.squareSize && w.axisIndex < w.squareSize
}
