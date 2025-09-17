package wrapper

import (
	"fmt"
	"runtime"
	"sync"

	"github.com/celestiaorg/go-square/v3/share"
	"github.com/celestiaorg/nmt"
	"github.com/celestiaorg/rsmt2d"

	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
)

// fixedTreePool provides a fixed-size pool of bufferedTree instances
type fixedTreePool struct {
	availableNMTs chan *bufferedTree
	opts          []nmt.Option
	squareSize    uint64
}

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

func (p *fixedTreePool) acquire() *bufferedTree {
	return <-p.availableNMTs
}

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

func NewTreePool(squareSize uint64, poolSize int, opts ...nmt.Option) *TreePool {
	return &TreePool{
		squareSize: squareSize,
		opts:       opts,
		poolSize:   poolSize,
		treePool:   newFixedTreePool(poolSize, squareSize, opts),
	}
}

func (f *TreePool) BufferSize() int {
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

// newBufferedTree creates a new bufferedTree with buffer pooling support
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

// Push adds the provided data to the underlying NamespaceMerkleTree for bufferedTree
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

// Root fulfills the rsmt2d.Tree interface for bufferedTree
func (w *bufferedTree) Root() ([]byte, error) {
	defer w.pool.release(w)
	return w.tree.FastRoot()
}

// incrementShareIndex increments the share index by one.
func (w *bufferedTree) incrementShareIndex() {
	w.shareIndex++
}

// isQuadrantZero returns true if the current share index and axis index are both
// in the original data square.
func (w *bufferedTree) isQuadrantZero() bool {
	return w.shareIndex < w.squareSize && w.axisIndex < w.squareSize
}

var _ rsmt2d.Tree = &bufferedTree{}
