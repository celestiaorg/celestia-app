package wrapper

import (
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/celestiaorg/rsmt2d"
	"github.com/stretchr/testify/require"
)

// acquireTimeout bounds how long a test waits for an operation that must not
// block.
const acquireTimeout = 5 * time.Second

// newShareWithNamespaceID returns a share in version 0 namespace id.
func newShareWithNamespaceID(id byte) []byte {
	s := make([]byte, share.ShareSize)
	s[share.NamespaceSize-1] = id
	return s
}

// descendingNamespaceSquare returns a 2x2 original data square whose first row
// has descending namespaces, so computing its row root fails inside nmt.Push
// and rsmt2d abandons the tree without calling Root.
func descendingNamespaceSquare() [][]byte {
	return [][]byte{
		newShareWithNamespaceID(0x05), newShareWithNamespaceID(0x01),
		newShareWithNamespaceID(0x06), newShareWithNamespaceID(0x07),
	}
}

// ascendingNamespaceSquare returns a valid 2x2 original data square.
func ascendingNamespaceSquare() [][]byte {
	return [][]byte{
		newShareWithNamespaceID(0x01), newShareWithNamespaceID(0x02),
		newShareWithNamespaceID(0x03), newShareWithNamespaceID(0x04),
	}
}

// ascendingNamespaceShares returns a valid originalSize x originalSize original
// data square whose namespaces ascend along every row and column.
func ascendingNamespaceShares(originalSize int) [][]byte {
	shares := make([][]byte, 0, originalSize*originalSize)
	for i := range originalSize * originalSize {
		shares = append(shares, newShareWithNamespaceID(byte(i)))
	}
	return shares
}

// computeRoots extends the square using the pool and computes its roots, like
// PrepareProposal and ProcessProposal do. finished is false if the computation
// blocked for longer than acquireTimeout.
func computeRoots(shares [][]byte, pool *TreePool) (finished bool, err error) {
	done := make(chan error, 1)
	go func() {
		eds, err := rsmt2d.ComputeExtendedDataSquareWithBuffer(shares, appconsts.DefaultCodec(), pool)
		if err != nil {
			done <- err
			return
		}
		if _, err := eds.RowRoots(); err != nil {
			done <- err
			return
		}
		_, err = eds.ColRoots()
		done <- err
	}()

	select {
	case err := <-done:
		return true, err
	case <-time.After(acquireTimeout):
		return false, nil
	}
}

// TestTreePoolAcquireDoesNotBlockWhenEmpty asserts that acquire allocates a tree
// instead of blocking when the pool is empty.
func TestTreePoolAcquireDoesNotBlockWhenEmpty(t *testing.T) {
	poolSize := 2
	pool, err := NewTreePool(2, poolSize)
	require.NoError(t, err)

	// Drop every tree on the floor, the way rsmt2d abandons one.
	for range poolSize {
		require.NotNil(t, pool.acquire())
	}
	require.Empty(t, pool.availableNMTs)

	acquired := make(chan *resizeableBufferTree, 1)
	go func() { acquired <- pool.acquire() }()

	select {
	case tree := <-acquired:
		require.NotNil(t, tree)
		// The freshly allocated tree refills the pool once it is released.
		pool.release(tree)
		require.Len(t, pool.availableNMTs, 1)
	case <-time.After(acquireTimeout):
		t.Fatal("acquire blocked on an empty pool")
	}
}

// TestTreePoolReleaseDoesNotBlockWhenFull asserts that release drops a tree
// instead of blocking when the pool is full.
func TestTreePoolReleaseDoesNotBlockWhenFull(t *testing.T) {
	pool, err := NewTreePool(2, 1)
	require.NoError(t, err)

	pooled := pool.acquire()
	allocated := pool.acquire() // the pool is empty, so this one is allocated
	pool.release(pooled)
	require.Len(t, pool.availableNMTs, 1)

	released := make(chan struct{})
	go func() {
		pool.release(allocated)
		close(released)
	}()

	select {
	case <-released:
		require.Len(t, pool.availableNMTs, 1)
	case <-time.After(acquireTimeout):
		t.Fatal("release blocked on a full pool")
	}
}

// countingPool wraps a TreePool and records how many trees rsmt2d holds at
// once, plus how many distinct trees it is ever handed.
type countingPool struct {
	*TreePool

	mu       sync.Mutex
	live     map[*resizeableBufferTree]struct{}
	peakLive int
	distinct map[*resizeableBufferTree]struct{}
}

func newCountingPool(pool *TreePool) *countingPool {
	return &countingPool{
		TreePool: pool,
		live:     make(map[*resizeableBufferTree]struct{}),
		distinct: make(map[*resizeableBufferTree]struct{}),
	}
}

func (c *countingPool) NewConstructor(squareSize uint) rsmt2d.TreeConstructorFn {
	construct := c.TreePool.NewConstructor(squareSize)
	return func(axis rsmt2d.Axis, axisIndex uint) rsmt2d.Tree {
		tree := construct(axis, axisIndex).(*resizeableBufferTree)
		c.mu.Lock()
		defer c.mu.Unlock()
		c.live[tree] = struct{}{}
		c.distinct[tree] = struct{}{}
		c.peakLive = max(c.peakLive, len(c.live))
		return countingTree{tree: tree, pool: c}
	}
}

// countingTree marks its tree as no longer live once rsmt2d takes its root.
type countingTree struct {
	tree *resizeableBufferTree
	pool *countingPool
}

func (t countingTree) Push(data []byte) error { return t.tree.Push(data) }

func (t countingTree) Root() ([]byte, error) {
	t.pool.mu.Lock()
	delete(t.pool.live, t.tree)
	t.pool.mu.Unlock()
	return t.tree.Root()
}

// TestTreePoolBoundsLiveTreesUnderConcurrency asserts that the pool still caps
// live trees at poolSize when rsmt2d drives it. rsmt2d limits its root
// computation errgroup to TreeCount(), so it never asks for more trees than the
// pool holds and acquire never reaches its allocation path.
func TestTreePoolBoundsLiveTreesUnderConcurrency(t *testing.T) {
	const (
		poolSize     = 4
		originalSize = 8 // 8x8 original data square, so rsmt2d computes 32 roots
	)
	pool, err := NewTreePool(originalSize, poolSize)
	require.NoError(t, err)
	counting := newCountingPool(pool)

	eds, err := rsmt2d.ComputeExtendedDataSquareWithBuffer(ascendingNamespaceShares(originalSize), appconsts.DefaultCodec(), counting)
	require.NoError(t, err)
	_, err = eds.RowRoots()
	require.NoError(t, err)
	_, err = eds.ColRoots()
	require.NoError(t, err)

	counting.mu.Lock()
	defer counting.mu.Unlock()
	require.LessOrEqual(t, counting.peakLive, poolSize, "rsmt2d held more trees at once than the pool size")
	require.LessOrEqual(t, len(counting.distinct), poolSize, "the pool handed out more distinct trees than the pool size")
}

// TestTreePoolSurvivesFailedRootComputations asserts that a valid square can
// still be processed after more failed root computations than the pool holds
// trees, because each failure abandons a tree.
func TestTreePoolSurvivesFailedRootComputations(t *testing.T) {
	poolSize := 4
	pool, err := NewTreePool(2, poolSize)
	require.NoError(t, err)

	for i := range poolSize + 1 {
		finished, err := computeRoots(descendingNamespaceSquare(), pool)
		require.True(t, finished, "failed root computation %d did not return", i+1)
		require.Error(t, err)
	}

	finished, err := computeRoots(ascendingNamespaceSquare(), pool)
	require.True(t, finished, "computing the roots of a valid square blocked forever")
	require.NoError(t, err)
}
