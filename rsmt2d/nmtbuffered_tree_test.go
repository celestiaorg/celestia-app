package rsmt2d

import (
	"bytes"
	"crypto/sha256"
	"fmt"

	"github.com/celestiaorg/nmt"
)

// treePool provides a fixed-size pool of resizeableBufferTree instances.
type treePool struct {
	availableNMTs chan *resizeableBufferTree
	opts          []nmt.Option
	poolSize      int
}

// newTreePool creates a new treePool with the specified configuration.
func newTreePool(initSquareSize uint, poolSize int, opts ...nmt.Option) *treePool {
	pool := &treePool{
		availableNMTs: make(chan *resizeableBufferTree, poolSize),
		opts:          opts,
		poolSize:      poolSize,
	}

	// initialize the pool with trees configured for initSquareSize
	for i := 0; i < poolSize; i++ {
		pool.availableNMTs <- newResizeableBufferTree(initSquareSize, 0, pool, opts...)
	}

	return pool
}

// acquire retrieves a resizeableBufferTree from the pool.
func (p *treePool) acquire() *resizeableBufferTree {
	return <-p.availableNMTs
}

// release returns a resizeableBufferTree to the pool for reuse.
func (p *treePool) release(tree *resizeableBufferTree) {
	p.availableNMTs <- tree
}

// TreeCount returns the number of trees in the pool.
func (p *treePool) TreeCount() int {
	return p.poolSize
}

// NewConstructor returns a tree constructor function that uses the pool with the specified square size.
func (p *treePool) NewConstructor(squareSize uint) TreeConstructorFn {
	return func(_ Axis, axisIndex uint) Tree {
		tree := p.acquire()
		tree.resize(squareSize)
		tree.reset(axisIndex)

		return tree
	}
}

var _ Tree = &resizeableBufferTree{}

// resizeableBufferTree is a wrapper around NamespaceMerkleTree with buffer pooling support
// for efficient memory management in high-throughput scenarios.
type resizeableBufferTree struct {
	squareSize    uint // note: this refers to the width of the original square before erasure-coded
	maxSquareSize uint // maximum square size this tree's buffer can handle without reallocation
	options       []nmt.Option
	tree          *nmt.NamespacedMerkleTree
	// axisIndex is the index of the axis (row or column) that this tree is on. This is passed
	// by rsmt2d and used to help determine which quadrant each leaf belongs to.
	axisIndex uint
	// shareIndex is the index of the share in a row or column that is being
	// pushed to the tree. It is expected to be in the range: 0 <= shareIndex <
	// 2*squareSize. shareIndex is used to help determine which quadrant each
	// leaf belongs to, along with keeping track of how many leaves have been
	// added to the tree so far.
	shareIndex      uint
	namespaceSize   int
	parityNamespace []byte
	pool            *treePool // reference to the pool this tree belongs to
	buffer          []byte    // Pre-allocated buffer for share data
	bufferEntrySize int
}

// newResizeableBufferTree creates a new resizeableBufferTree with pre-allocated buffer and pool reference.
func newResizeableBufferTree(maxSquareSize uint, axisIndex uint, pool *treePool, options ...nmt.Option) *resizeableBufferTree {
	if maxSquareSize == 0 {
		panic("cannot create a resizeableBufferTree of maxSquareSize == 0")
	}
	if pool == nil {
		panic("cannot create a resizeableBufferTree of pool == nil")
	}
	opts := &nmt.Options{}
	for _, setter := range options {
		setter(opts)
	}
	options = append(options, nmt.IgnoreMaxNamespace(true), nmt.ReuseBuffers(true))
	tree := nmt.New(sha256.New(), options...)
	namespaceSize := int(opts.NamespaceIDSize)
	entrySize := shareSize + namespaceSize
	return &resizeableBufferTree{
		squareSize:      maxSquareSize,
		maxSquareSize:   maxSquareSize,
		options:         options,
		tree:            tree,
		pool:            pool,
		bufferEntrySize: entrySize,
		namespaceSize:   namespaceSize,
		parityNamespace: bytes.Repeat([]byte{0xFF}, namespaceSize),
		axisIndex:       axisIndex,
		buffer:          make([]byte, 2*maxSquareSize*uint(entrySize)),
		shareIndex:      0,
	}
}

// Push adds share data to the tree using the pre-allocated buffer to avoid memory allocation.
func (t *resizeableBufferTree) Push(data []byte) error {
	if t.axisIndex+1 > 2*t.squareSize || t.shareIndex+1 > 2*t.squareSize {
		return fmt.Errorf("pushed past predetermined square size: boundary at %d index at %d %d", 2*t.squareSize, t.axisIndex, t.shareIndex)
	}
	if len(data) < t.namespaceSize {
		return fmt.Errorf("data is too short to contain namespace ID")
	}
	var (
		nidAndData []byte
		nidDataLen = t.namespaceSize + len(data)
	)

	if nidDataLen > t.bufferEntrySize {
		return fmt.Errorf("data is too large to be used with allocated buffer")
	}
	offset := int(t.shareIndex) * t.bufferEntrySize
	nidAndData = t.buffer[offset : offset+nidDataLen]

	copy(nidAndData[t.namespaceSize:], data)
	// use the parity namespace if the cell is not in Q0 of the extended data square
	if t.isQuadrantZero() {
		copy(nidAndData[:t.namespaceSize], data[:t.namespaceSize])
	} else {
		copy(nidAndData[:t.namespaceSize], t.parityNamespace)
	}
	err := t.tree.Push(nidAndData)
	if err != nil {
		return err
	}
	t.incrementShareIndex()
	return nil
}

// Root calculates the tree root using FastRoot and releases the tree back to the pool.
func (t *resizeableBufferTree) Root() ([]byte, error) {
	defer t.pool.release(t)
	return t.tree.Root()
}

// incrementShareIndex advances to the next share position in the tree.
func (t *resizeableBufferTree) incrementShareIndex() {
	t.shareIndex++
}

// isQuadrantZero checks if the current position is in the original (non-parity) data quadrant.
func (t *resizeableBufferTree) isQuadrantZero() bool {
	return t.shareIndex < t.squareSize && t.axisIndex < t.squareSize
}

// resize adjusts the tree's square size and resizes the buffer if the new size exceeds the current maximum capacity.
func (t *resizeableBufferTree) resize(squareSize uint) {
	if t.squareSize != squareSize {
		t.squareSize = squareSize
		if squareSize > t.maxSquareSize {
			entrySize := t.bufferEntrySize
			t.buffer = make([]byte, 2*squareSize*uint(entrySize))
			t.maxSquareSize = squareSize
		}
	}
}

// reset reinitializes the resizeableBufferTree for reuse.
func (t *resizeableBufferTree) reset(axisIndex uint) {
	t.axisIndex = axisIndex
	t.shareIndex = 0
	t.tree.Reset()
}
