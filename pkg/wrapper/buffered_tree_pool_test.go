package wrapper

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/celestiaorg/rsmt2d"
	"github.com/stretchr/testify/require"
)

// acquireTimeout bounds how long a test waits for a pool operation that must
// not block.
const acquireTimeout = 5 * time.Second

// newShareWithNamespaceID returns a share whose namespace is version 0 with the
// provided last byte of the namespace ID.
func newShareWithNamespaceID(id byte) []byte {
	s := make([]byte, share.ShareSize)
	s[share.NamespaceSize-1] = id
	return s
}

// descendingNamespaceSquare returns the four shares of a 2x2 original data
// square whose first row has descending namespaces. Computing the root of row 0
// therefore fails inside nmt.Push, which is the error path that returns to
// rsmt2d without ever calling Root.
func descendingNamespaceSquare() [][]byte {
	return [][]byte{
		newShareWithNamespaceID(0x05), newShareWithNamespaceID(0x01),
		newShareWithNamespaceID(0x06), newShareWithNamespaceID(0x07),
	}
}

// ascendingNamespaceSquare returns the four shares of a valid 2x2 original data
// square.
func ascendingNamespaceSquare() [][]byte {
	return [][]byte{
		newShareWithNamespaceID(0x01), newShareWithNamespaceID(0x02),
		newShareWithNamespaceID(0x03), newShareWithNamespaceID(0x04),
	}
}

// computeRoots extends the square using the pool and computes its roots, the
// same pair of calls that PrepareProposal and ProcessProposal run. It reports
// false if the computation did not finish within acquireTimeout.
func computeRoots(shares [][]byte, pool *TreePool) (err error, finished bool) {
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
		return err, true
	case <-time.After(acquireTimeout):
		return nil, false
	}
}

// TestTreePoolAcquireDoesNotBlockWhenEmpty asserts that acquiring from a drained
// pool allocates a tree instead of blocking. rsmt2d abandons a tree whenever a
// root computation returns early, so the pool has to tolerate losing trees.
func TestTreePoolAcquireDoesNotBlockWhenEmpty(t *testing.T) {
	poolSize := 2
	pool, err := NewTreePool(2, poolSize)
	require.NoError(t, err)

	// Drop every tree on the floor, the way an abandoned tree is lost.
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

// TestTreePoolReleaseDoesNotBlockWhenFull asserts that releasing a tree the pool
// has no room for drops it rather than blocking the caller.
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

// TestTreePoolSurvivesFailedRootComputations asserts that a valid square can
// still be processed after more failed root computations than the pool holds
// trees. Each failure abandons a tree, so a pool that neither reclaims nor
// replaces them would block here forever, inside the ABCI call.
func TestTreePoolSurvivesFailedRootComputations(t *testing.T) {
	poolSize := 4
	pool, err := NewTreePool(2, poolSize)
	require.NoError(t, err)

	for i := range poolSize + 1 {
		err, finished := computeRoots(descendingNamespaceSquare(), pool)
		require.True(t, finished, "failed root computation %d did not return", i+1)
		require.Error(t, err)
	}

	err, finished := computeRoots(ascendingNamespaceSquare(), pool)
	require.True(t, finished, "computing the roots of a valid square blocked forever")
	require.NoError(t, err)
}
