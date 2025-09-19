package wrapper

import (
	"bytes"
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/test/util/testfactory"
	"github.com/celestiaorg/rsmt2d"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTreePool_AcquireRelease(t *testing.T) {
	squareSize := uint(8)
	poolSize := 4
	pool := NewTreePool(squareSize, poolSize)

	trees := make([]*resizeableBufferTree, 0, poolSize)

	for i := 0; i < poolSize; i++ {
		constructor := pool.NewConstructor(squareSize)
		tree := constructor(rsmt2d.Row, uint(i))
		nmt, ok := tree.(*resizeableBufferTree)
		require.True(t, ok)
		trees = append(trees, nmt)
	}

	for _, tree := range trees {
		// Root() which internally calls release()
		_, _ = tree.Root()
	}

	for i := 0; i < poolSize; i++ {
		constructor := pool.NewConstructor(squareSize)
		tree := constructor(rsmt2d.Row, uint(i))
		require.NotNil(t, tree)
	}
}

func TestResizeableBufferTree_WithPoolReuse(t *testing.T) {
	squareSize := uint(8)
	poolSize := 4
	pool := NewTreePool(squareSize, poolSize)

	data := testfactory.GenerateRandNamespacedRawData(int(squareSize * 2))

	constructor := pool.NewConstructor(squareSize)
	tree := constructor(rsmt2d.Row, 0)

	for _, d := range data {
		err := tree.Push(d)
		require.NoError(t, err)
	}

	root, err := tree.Root()
	require.NoError(t, err)
	require.NotEmpty(t, root)

	tree2 := constructor(rsmt2d.Row, 0) // Use same axis index for same root
	require.NotNil(t, tree2)

	for _, d := range data {
		err := tree2.Push(d)
		require.NoError(t, err)
	}

	root2, err := tree2.Root()
	require.NoError(t, err)
	require.NotEmpty(t, root2)

	// Verify that both trees produce the same root for the same data
	assert.True(t, bytes.Equal(root, root2), "Reused tree should produce the same root for the same data")
}

func TestComputeExtendedDataSquare_WithAndWithoutPool(t *testing.T) {
	testCases := []struct {
		name       string
		squareSize int
	}{
		{
			name:       "small square 4x4",
			squareSize: 4,
		},
		{
			name:       "medium square 16x16",
			squareSize: 16,
		},
		{
			name:       "large square 64x64",
			squareSize: 64,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := testfactory.GenerateRandNamespacedRawData(tc.squareSize * tc.squareSize)

			edsWithoutPool, err := rsmt2d.ComputeExtendedDataSquare(
				data,
				appconsts.DefaultCodec(),
				NewConstructor(uint64(tc.squareSize)),
			)
			require.NoError(t, err)

			pool := NewTreePool(uint(tc.squareSize), runtime.NumCPU()*4)
			edsWithPool, err := rsmt2d.ComputeExtendedDataSquare(
				data,
				appconsts.DefaultCodec(),
				pool.NewConstructor(uint(tc.squareSize)),
			)
			require.NoError(t, err)

			rowRootsWithoutPool, err := edsWithoutPool.RowRoots()
			require.NoError(t, err)
			rowRootsWithPool, err := edsWithPool.RowRoots()
			require.NoError(t, err)

			assert.Equal(t, len(rowRootsWithoutPool), len(rowRootsWithPool))
			for i := range rowRootsWithoutPool {
				assert.True(t, bytes.Equal(rowRootsWithoutPool[i], rowRootsWithPool[i]),
					"Row root %d should be equal", i)
			}

			colRootsWithoutPool, err := edsWithoutPool.ColRoots()
			require.NoError(t, err)
			colRootsWithPool, err := edsWithPool.ColRoots()
			require.NoError(t, err)

			assert.Equal(t, len(colRootsWithoutPool), len(colRootsWithPool))
			for i := range colRootsWithoutPool {
				assert.True(t, bytes.Equal(colRootsWithoutPool[i], colRootsWithPool[i]),
					"Column root %d should be equal", i)
			}
		})
	}
}

func TestTreePool_ConcurrentAccess(t *testing.T) {
	squareSize := uint(16)
	poolSize := 8
	pool := NewTreePool(squareSize, poolSize)

	// Generate test data for multiple trees
	numTrees := 20
	treeData := make([][][]byte, numTrees)
	for i := 0; i < numTrees; i++ {
		treeData[i] = testfactory.GenerateRandNamespacedRawData(int(squareSize * 2))
	}

	// First, compute roots sequentially using standard trees (without pool/buffer)
	sequentialRoots := make([][]byte, numTrees)
	for i := 0; i < numTrees; i++ {
		// Use the standard constructor without pool
		tree := NewErasuredNamespacedMerkleTree(uint64(squareSize), uint(i))

		for _, d := range treeData[i] {
			err := tree.Push(d)
			require.NoError(t, err)
		}

		root, err := tree.Root()
		require.NoError(t, err)
		sequentialRoots[i] = root
	}

	// Now compute the same roots concurrently using the pool
	concurrentRoots := make([][]byte, numTrees)
	var wg sync.WaitGroup
	wg.Add(numTrees)

	for i := 0; i < numTrees; i++ {
		go func(index int) {
			defer wg.Done()

			constructor := pool.NewConstructor(squareSize)
			tree := constructor(rsmt2d.Row, uint(index))

			for _, d := range treeData[index] {
				require.NoError(t, tree.Push(d))
			}

			root, err := tree.Root()
			require.NoError(t, err)
			concurrentRoots[index] = root
		}(i)
	}

	wg.Wait()

	// Verify that all concurrent roots (with pool) match sequential roots (without pool)
	for i := 0; i < numTrees; i++ {
		assert.True(t, bytes.Equal(sequentialRoots[i], concurrentRoots[i]),
			"Tree %d: concurrent root (with pool) should match sequential root (without pool)", i)
	}
}

func TestResizeableBufferTree_RootConsistency(t *testing.T) {
	squareSize := uint(8)

	// Test with ErasuredNamespacedMerkleTree (no buffer)
	tree1 := NewErasuredNamespacedMerkleTree(uint64(squareSize), 0)

	data := testfactory.GenerateRandNamespacedRawData(int(squareSize * 2))

	for _, d := range data {
		err := tree1.Push(d)
		require.NoError(t, err)
	}

	root1, err := tree1.Root()
	require.NoError(t, err)

	// Test with resizeableBufferTree (with buffer) - acquire from pool properly
	pool := NewTreePool(squareSize, 1)
	constructor := pool.NewConstructor(squareSize)
	tree2 := constructor(rsmt2d.Row, 0)

	for _, d := range data {
		err := tree2.Push(d)
		require.NoError(t, err)
	}

	root2, err := tree2.Root()
	require.NoError(t, err)

	assert.True(t, bytes.Equal(root1, root2), "resizeableBufferTree should produce the same root as ErasuredNamespacedMerkleTree")
}

func BenchmarkExtendedDataSquare_WithPool(b *testing.B) {
	squareSizes := []int{4, 8, 16, 32, 64, 128}

	for _, size := range squareSizes {
		b.Run(fmt.Sprintf("SquareSize-%d", size), func(b *testing.B) {
			data := testfactory.GenerateRandNamespacedRawData(size * size)
			pool := NewTreePool(uint(size), runtime.NumCPU()*4)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				square, err := rsmt2d.ComputeExtendedDataSquare(
					data,
					appconsts.DefaultCodec(),
					pool.NewConstructor(uint(size)),
				)
				require.NoError(b, err)
				_, err = square.RowRoots()
				require.NoError(b, err)
			}
		})
	}
}

func BenchmarkExtendedDataSquare_WithoutPool(b *testing.B) {
	squareSizes := []int{4, 8, 16, 32, 64, 128}

	for _, size := range squareSizes {
		b.Run(fmt.Sprintf("SquareSize-%d", size), func(b *testing.B) {
			data := testfactory.GenerateRandNamespacedRawData(size * size)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				square, err := rsmt2d.ComputeExtendedDataSquare(
					data,
					appconsts.DefaultCodec(),
					NewConstructor(uint64(size)),
				)
				require.NoError(b, err)
				_, err = square.RowRoots()
				require.NoError(b, err)
			}
		})
	}
}
