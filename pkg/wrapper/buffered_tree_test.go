package wrapper

import (
	"bytes"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v8/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v8/test/util/testfactory"
	"github.com/celestiaorg/rsmt2d"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var standardSizes = []int{2, 4, 8, 16, 32, 64, 128, 256, 512}

func TestTreePool_AcquireRelease(t *testing.T) {
	squareSize := uint(512)
	poolSize := 100
	pool, err := NewTreePool(squareSize, poolSize)
	require.NoError(t, err)

	trees := make([]*resizeableBufferTree, 0, poolSize)

	for i := range poolSize {
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

	for i := range poolSize {
		constructor := pool.NewConstructor(squareSize)
		tree := constructor(rsmt2d.Row, uint(i))
		require.NotNil(t, tree)
	}
}

func TestResizeableBufferTree_WithPoolReuse(t *testing.T) {
	for _, size := range standardSizes {
		t.Run(fmt.Sprintf("size_%d", size), func(t *testing.T) {
			squareSize := uint(size)
			poolSize := 4
			pool, err := NewTreePool(squareSize, poolSize)
			require.NoError(t, err)

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
			assert.True(t, bytes.Equal(root, root2),
				"Reused tree should produce the same root for the same data (size %d)", size)
		})
	}
}

func TestComputeExtendedDataSquare_WithAndWithoutPool(t *testing.T) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	testSquareSize := func(t *testing.T, sizes []int, pool *TreePool) {
		for _, squareSize := range sizes {
			t.Run(fmt.Sprintf("size_%d", squareSize), func(t *testing.T) {
				// Create a new pool for each iteration (no reuse)
				comparePoolAndNonPoolEDS(t, squareSize, pool)
			})
		}
	}

	genRandomSizes := func(size, max int) []int {
		sizes := make([]int, size)
		for i := range sizes {
			sizes[i] = rng.Intn(max) + 1
		}
		return sizes
	}

	t.Run("sizes - reuse small pool", func(t *testing.T) {
		t.Skip("this takes too long to run on github runners")
		pool, err := NewTreePool(2, runtime.NumCPU()*4)
		require.NoError(t, err)
		testSquareSize(t, standardSizes, pool)
		pool, err = NewTreePool(2, runtime.NumCPU()*4)
		require.NoError(t, err)
		// test is not very fast, so we do less fuzzing
		testSquareSize(t, genRandomSizes(6, 512), pool)
	})

	t.Run("sizes - reuse large pool", func(t *testing.T) {
		// this is the main use case for prepare proposal and process proposal
		pool, err := NewTreePool(512, runtime.NumCPU()*4)
		require.NoError(t, err)
		testSquareSize(t, standardSizes, pool)
		t.Run("reuse large pool random sizes", func(t *testing.T) {
			t.Skip("this takes too long to run on github runners")
			testSquareSize(t, genRandomSizes(6, 512), pool)
		})
	})
}

func comparePoolAndNonPoolEDS(t *testing.T, squareSize int, pool *TreePool) {
	data := testfactory.GenerateRandNamespacedRawData(squareSize * squareSize)

	edsWithoutPool, err := rsmt2d.ComputeExtendedDataSquare(
		data,
		appconsts.DefaultCodec(),
		NewConstructor(uint64(squareSize)),
	)
	require.NoError(t, err)

	// If no pool is provided, create a new one for this test
	if pool == nil {
		pool, err = NewTreePool(uint(squareSize), runtime.NumCPU()*4)
		require.NoError(t, err)
	}

	edsWithPool, err := rsmt2d.ComputeExtendedDataSquareWithBuffer(
		data,
		appconsts.DefaultCodec(),
		pool,
	)
	require.NoError(t, err)

	rowRootsWithoutPool, err := edsWithoutPool.RowRoots()
	require.NoError(t, err)
	rowRootsWithPool, err := edsWithPool.RowRoots()
	require.NoError(t, err)

	assert.Equal(t, len(rowRootsWithoutPool), len(rowRootsWithPool))
	for i := range rowRootsWithoutPool {
		assert.True(t, bytes.Equal(rowRootsWithoutPool[i], rowRootsWithPool[i]),
			"Row root %d should be equal for square size %d", i, squareSize)
	}

	colRootsWithoutPool, err := edsWithoutPool.ColRoots()
	require.NoError(t, err)
	colRootsWithPool, err := edsWithPool.ColRoots()
	require.NoError(t, err)

	assert.Equal(t, len(colRootsWithoutPool), len(colRootsWithPool))
	for i := range colRootsWithoutPool {
		assert.True(t, bytes.Equal(colRootsWithoutPool[i], colRootsWithPool[i]),
			"Column root %d should be equal for square size %d", i, squareSize)
	}
}

func TestTreePool_ConcurrentAccess(t *testing.T) {
	squareSize := uint(16)
	poolSize := 8
	pool, err := NewTreePool(squareSize, poolSize)
	require.NoError(t, err)

	// generate test data for multiple trees
	numTrees := 20
	treeData := make([][][]byte, numTrees)
	for i := range numTrees {
		treeData[i] = testfactory.GenerateRandNamespacedRawData(int(squareSize * 2))
	}

	// first, compute roots sequentially using standard trees (without pool/buffer)
	sequentialRoots := make([][]byte, numTrees)
	for i := range numTrees {
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

	// now compute the same roots concurrently using the pool
	concurrentRoots := make([][]byte, numTrees)
	var wg sync.WaitGroup
	wg.Add(numTrees)

	for i := range numTrees {
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

	// verify that all concurrent roots (with pool) match sequential roots (without pool)
	for i := range numTrees {
		assert.True(t, bytes.Equal(sequentialRoots[i], concurrentRoots[i]),
			"Tree %d: concurrent root (with pool) should match sequential root (without pool)", i)
	}
}

func BenchmarkExtendedDataSquare_WithPool(b *testing.B) {
	for _, size := range standardSizes {
		b.Run(fmt.Sprintf("SquareSize-%d", size), func(b *testing.B) {
			data := testfactory.GenerateRandNamespacedRawData(size * size)
			pool, err := NewTreePool(uint(size), runtime.NumCPU()*4)
			require.NoError(b, err)

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				square, err := rsmt2d.ComputeExtendedDataSquareWithBuffer(
					data,
					appconsts.DefaultCodec(),
					pool,
				)
				require.NoError(b, err)
				_, err = square.RowRoots()
				require.NoError(b, err)
			}
		})
	}
}

func BenchmarkExtendedDataSquare_WithoutPool(b *testing.B) {
	for _, size := range standardSizes {
		b.Run(fmt.Sprintf("SquareSize-%d", size), func(b *testing.B) {
			data := testfactory.GenerateRandNamespacedRawData(size * size)

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
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
