package wrapper

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/celestiaorg/nmt"
	"github.com/celestiaorg/rsmt2d"
	"github.com/stretchr/testify/require"
)

// acquireTimeout bounds how long a test waits for an operation that must not
// block.
const acquireTimeout = 5 * time.Second

// errTimeout reports that computeRoots blocked for longer than acquireTimeout.
var errTimeout = errors.New("root computation timed out")

func newShareWithNamespaceID(id byte) []byte {
	s := make([]byte, share.ShareSize)
	s[share.NamespaceSize-1] = id
	return s
}

// descendingNamespaceSquare returns a 2x2 square whose first row has descending
// namespaces, so its row root fails inside nmt.Push and rsmt2d abandons the
// tree without calling Root.
func descendingNamespaceSquare() [][]byte {
	return [][]byte{
		newShareWithNamespaceID(0x05), newShareWithNamespaceID(0x01),
		newShareWithNamespaceID(0x06), newShareWithNamespaceID(0x07),
	}
}

// computeRoots extends the square and computes its roots, like the proposal
// handlers do. It returns errTimeout if that blocked for longer than
// acquireTimeout.
func computeRoots(shares [][]byte, pool *TreePool) error {
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
		return err
	case <-time.After(acquireTimeout):
		return errTimeout
	}
}

func TestTreePoolAcquireDoesNotBlockWhenEmpty(t *testing.T) {
	poolSize := 2
	pool, err := NewTreePool(2, poolSize)
	require.NoError(t, err)

	// Drop every tree on the floor, the way rsmt2d abandons one.
	for range poolSize {
		require.NotNil(t, pool.acquire(2))
	}
	require.Empty(t, pool.availableNMTs)

	acquired := make(chan *resizeableBufferTree, 1)
	go func() { acquired <- pool.acquire(2) }()

	select {
	case tree := <-acquired:
		require.NotNil(t, tree)
		pool.release(tree)
		require.Len(t, pool.availableNMTs, 1)
	case <-time.After(acquireTimeout):
		t.Fatal("acquire blocked on an empty pool")
	}
}

func TestTreePoolReleaseDoesNotBlockWhenFull(t *testing.T) {
	pool, err := NewTreePool(2, 1)
	require.NoError(t, err)

	pooled := pool.acquire(2)
	allocated := pool.acquire(2) // the pool is empty, so this one is allocated
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

// countingPool records the distinct trees rsmt2d is handed.
type countingPool struct {
	*TreePool

	mu       sync.Mutex
	distinct map[*resizeableBufferTree]struct{}
}

func newCountingPool(pool *TreePool) *countingPool {
	return &countingPool{
		TreePool: pool,
		distinct: make(map[*resizeableBufferTree]struct{}),
	}
}

func (c *countingPool) NewConstructor(squareSize uint) rsmt2d.TreeConstructorFn {
	construct := c.TreePool.NewConstructor(squareSize)
	return func(axis rsmt2d.Axis, axisIndex uint) rsmt2d.Tree {
		tree := construct(axis, axisIndex).(*resizeableBufferTree)
		c.mu.Lock()
		defer c.mu.Unlock()
		c.distinct[tree] = struct{}{}
		return tree
	}
}

// TestTreePoolReusesTreesUnderConcurrency asserts that acquire's allocation
// path does not uncap memory: rsmt2d limits its root computation errgroup to
// TreeCount(), so it never asks for more trees than the pool holds.
func TestTreePoolReusesTreesUnderConcurrency(t *testing.T) {
	const (
		poolSize     = 4
		originalSize = 8 // 8x8 square, so rsmt2d computes 32 roots
	)
	pool, err := NewTreePool(originalSize, poolSize)
	require.NoError(t, err)
	counting := newCountingPool(pool)

	shares := testfactory.GenerateRandNamespacedRawData(originalSize * originalSize)
	eds, err := rsmt2d.ComputeExtendedDataSquareWithBuffer(shares, appconsts.DefaultCodec(), counting)
	require.NoError(t, err)
	_, err = eds.RowRoots()
	require.NoError(t, err)
	_, err = eds.ColRoots()
	require.NoError(t, err)

	counting.mu.Lock()
	defer counting.mu.Unlock()
	require.LessOrEqual(t, len(counting.distinct), poolSize, "the pool handed out more distinct trees than the pool size")
}

// TestTreePoolSurvivesFailedRootComputations drives more failed root
// computations than the pool holds trees, each of which abandons a tree.
func TestTreePoolSurvivesFailedRootComputations(t *testing.T) {
	poolSize := 4
	pool, err := NewTreePool(2, poolSize)
	require.NoError(t, err)

	for i := range poolSize + 1 {
		err := computeRoots(descendingNamespaceSquare(), pool)
		require.Error(t, err)
		require.NotErrorIs(t, err, errTimeout, "failed root computation %d did not return", i+1)
	}

	err = computeRoots(testfactory.GenerateRandNamespacedRawData(4), pool)
	require.NoError(t, err, "computing the roots of a valid square blocked or failed")
}

// TestTreePoolAcquireIsSafeWithCallerOpts guards against concurrent acquires
// appending into a caller-provided option slice's backing array.
func TestTreePoolAcquireIsSafeWithCallerOpts(t *testing.T) {
	opts := make([]nmt.Option, 0, 8) // spare capacity, so an aliasing append races
	opts = append(opts, nmt.IgnoreMaxNamespace(true))
	pool, err := NewTreePool(2, 1, opts...)
	require.NoError(t, err)
	require.NotNil(t, pool.acquire(2)) // drain the pool so acquires must allocate

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			_ = pool.acquire(2)
		})
	}
	wg.Wait()
}
