package rsmt2d

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/merkletree"
	"github.com/celestiaorg/nmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	defaultNamespaceIDSize = 29
	mebibyte               = 1024 * 1024
)

func TestNewDataSquare(t *testing.T) {
	tests := []struct {
		name      string
		cells     [][]byte
		expected  [][][]byte
		shareSize uint
	}{
		{"1x1", [][]byte{{1, 2}}, [][][]byte{{{1, 2}}}, 2},
		{"2x2", [][]byte{{1, 2}, {3, 4}, {5, 6}, {7, 8}}, [][][]byte{{{1, 2}, {3, 4}}, {{5, 6}, {7, 8}}}, 2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := newDataSquare(test.cells, NewDefaultTree, test.shareSize)
			if err != nil {
				panic(err)
			}
			if !reflect.DeepEqual(result.squareRow, test.expected) {
				t.Errorf("newDataSquare failed for %v square", test.name)
			}
		})
	}
}

func TestInvalidDataSquareCreation(t *testing.T) {
	tests := []struct {
		name      string
		cells     [][]byte
		shareSize uint
	}{
		{"InconsistentShareNumber", [][]byte{{1, 2}, {3, 4}, {5, 6}}, 2},
		{"UnequalShareSize", [][]byte{{1, 2}, {3, 4}, {5, 6}, {7}}, 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newDataSquare(test.cells, NewDefaultTree, test.shareSize)
			if err == nil {
				t.Errorf("newDataSquare failed; shares accepted with %v", test.name)
			}
		})
	}
}

func TestSetCell(t *testing.T) {
	type testCase struct {
		name         string
		originalCell []byte
		newCell      []byte
		wantErr      bool
	}

	testCases := []testCase{
		{
			name:         "can set cell if originally nil",
			originalCell: nil,
			newCell:      []byte{42},
			wantErr:      false,
		},
		{
			name:         "expect error if cell is not originally nil",
			originalCell: []byte{1},
			wantErr:      true,
		},
		{
			name:         "expect error if new cell is not the correct share size",
			originalCell: nil,
			newCell:      []byte{1, 2}, // incorrect share size
			wantErr:      true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ds, err := newDataSquare([][]byte{tc.originalCell, {2}, {3}, {4}}, NewDefaultTree, 1)
			assert.NoError(t, err)

			err = ds.SetCell(0, 0, tc.newCell)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.newCell, ds.GetCell(0, 0))
			}
		})
	}
}

// Test_setCell verifies that setCell can overwrite cells without performing any
// input validation.
func Test_setCell(t *testing.T) {
	type testCase struct {
		name     string
		original []byte
		new      []byte
	}

	testCases := []testCase{
		{
			name:     "can set cell if originally nil",
			original: nil,
			new:      []byte{42},
		},
		{
			name:     "can set cell if originally some value",
			original: []byte{1},
			new:      []byte{42},
		},
		{
			name:     "can set cell to nil",
			original: []byte{1},
			new:      nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ds, err := newDataSquare([][]byte{tc.original, {2}, {3}, {4}}, NewDefaultTree, 1)
			assert.NoError(t, err)

			ds.setCell(0, 0, tc.new)
			assert.Equal(t, tc.new, ds.GetCell(0, 0))
		})
	}
}

func TestGetCell(t *testing.T) {
	ds, err := newDataSquare([][]byte{{1}, {2}, {3}, {4}}, NewDefaultTree, 1)
	if err != nil {
		panic(err)
	}

	cell := ds.GetCell(0, 0)
	cell[0] = 42

	if reflect.DeepEqual(ds.GetCell(0, 0), []byte{42}) {
		t.Errorf("GetCell failed to return an immutable copy of the cell")
	}
}

func TestFlattened(t *testing.T) {
	ds, err := newDataSquare([][]byte{{1}, {2}, {3}, {4}}, NewDefaultTree, 1)
	if err != nil {
		panic(err)
	}

	flattened := ds.Flattened()
	flattened[0] = []byte{42}

	if reflect.DeepEqual(ds.Flattened(), [][]byte{{42}, {2}, {3}, {4}}) {
		t.Errorf("Flattened failed to return an immutable copy")
	}
}

func TestExtendSquare(t *testing.T) {
	ds, err := newDataSquare([][]byte{{1, 2}}, NewDefaultTree, 2)
	if err != nil {
		panic(err)
	}
	err = ds.extendSquare(1, []byte{0, 0})
	if err != nil {
		panic(err)
	}
	if !reflect.DeepEqual(ds.squareRow, [][][]byte{{{1, 2}, {0, 0}}, {{0, 0}, {0, 0}}}) {
		t.Errorf("extendSquare failed; unexpected result when extending 1x1 square to 2x2 square")
	}
}

func TestInvalidSquareExtension(t *testing.T) {
	ds, err := newDataSquare([][]byte{{1, 2}}, NewDefaultTree, 2)
	if err != nil {
		panic(err)
	}
	err = ds.extendSquare(1, []byte{0})
	if err == nil {
		t.Errorf("extendSquare failed; error not returned when filler share size does not match data square share size")
	}
}

// Test_getRoots verifies that the row roots and column roots are equal for a 1x1
// square.
func Test_getRoots(t *testing.T) {
	result, err := newDataSquare([][]byte{{1, 2}}, NewDefaultTree, 2)
	assert.NoError(t, err)

	rowRoots, err := result.getRowRoots()
	assert.NoError(t, err)

	colRoots, err := result.getColRoots()
	assert.NoError(t, err)

	assert.Equal(t, rowRoots, colRoots)
}

func TestLazyRootGeneration(t *testing.T) {
	square, err := newDataSquare([][]byte{{1}, {2}, {3}, {4}}, NewDefaultTree, 1)
	if err != nil {
		panic(err)
	}

	var rowRoots [][]byte
	var colRoots [][]byte

	for i := uint(0); i < square.width; i++ {
		rowRoot, err := square.getRowRoot(i)
		assert.NoError(t, err)
		colRoot, err := square.getColRoot(i)
		assert.NoError(t, err)
		rowRoots = append(rowRoots, rowRoot)
		colRoots = append(colRoots, colRoot)
	}

	err = square.computeRoots()
	assert.NoError(t, err)

	assert.Equal(t, square.rowRoots, rowRoots)
	assert.Equal(t, square.colRoots, colRoots)
}

func TestComputeRoots(t *testing.T) {
	t.Run("default tree computeRoots() returns no error", func(t *testing.T) {
		square, err := newDataSquare([][]byte{{1}, {2}, {3}, {4}}, NewDefaultTree, 1)
		assert.NoError(t, err)
		err = square.computeRoots()
		assert.NoError(t, err)
	})
	t.Run("error tree computeRoots() returns an error", func(t *testing.T) {
		square, err := newDataSquare([][]byte{{1}}, newErrorTree, 1)
		assert.NoError(t, err)
		err = square.computeRoots()
		assert.Error(t, err)
	})
}

func TestRootAPI(t *testing.T) {
	square, err := newDataSquare([][]byte{{1}, {2}, {3}, {4}}, NewDefaultTree, 1)
	if err != nil {
		panic(err)
	}

	for i := uint(0); i < square.width; i++ {
		rowRoot, err := square.getRowRoot(i)
		assert.NoError(t, err)

		rowRoots, err := square.getRowRoots()
		assert.NoError(t, err)

		assert.Equal(t, rowRoots[i], rowRoot)

		colRoot, err := square.getColRoot(i)
		assert.NoError(t, err)

		colRoots, err := square.getColRoots()
		assert.NoError(t, err)

		assert.Equal(t, colRoots[i], colRoot)
	}
}

func TestDefaultTreeProofs(t *testing.T) {
	result, err := newDataSquare([][]byte{{1, 2}, {3, 4}, {5, 6}, {7, 8}}, NewDefaultTree, 2)
	if err != nil {
		panic(err)
	}
	_, proof, proofIndex, numLeaves, err := computeRowProof(result, 1, 1)
	if err != nil {
		t.Errorf("Got unexpected error: %v", err)
	}

	if len(proof) != 2 {
		t.Errorf("computing row proof for (1, 1) in 2x2 square failed; expecting proof set of length 2")
	}
	if proofIndex != 1 {
		t.Errorf("computing row proof for (1, 1) in 2x2 square failed; expecting proof index of 1")
	}
	if numLeaves != 2 {
		t.Errorf("computing row proof for (1, 1) in 2x2 square failed; expecting number of leaves to be 2")
	}
}

func Test_setRowSlice(t *testing.T) {
	type testCase struct {
		name    string
		newRow  [][]byte
		rowIdx  uint
		colIdx  uint
		want    [][]byte
		wantErr bool
	}
	testCases := []testCase{
		{
			name:    "overwrite the first row",
			newRow:  [][]byte{{5}, {6}},
			rowIdx:  0,
			colIdx:  0,
			want:    [][]byte{{5}, {6}, {3}, {4}},
			wantErr: false,
		},
		{
			name:    "overwrite the last row",
			newRow:  [][]byte{{5}, {6}},
			rowIdx:  1,
			colIdx:  0,
			want:    [][]byte{{1}, {2}, {5}, {6}},
			wantErr: false,
		},
		{
			name:    "returns an error if the new row has an invalid share size",
			newRow:  [][]byte{{5, 6}},
			rowIdx:  0,
			colIdx:  0,
			wantErr: true,
		},
		{
			name:    "returns an error if the new row would surpass the data square's width",
			newRow:  [][]byte{{5}, {6}},
			rowIdx:  0,
			colIdx:  1,
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		ds, err := newDataSquare([][]byte{{1}, {2}, {3}, {4}}, NewDefaultTree, 1)
		assert.NoError(t, err)
		err = ds.setRowSlice(tc.rowIdx, tc.colIdx, tc.newRow)

		if tc.wantErr {
			assert.Error(t, err)
			return
		}
		assert.NoError(t, err)
		assert.Equal(t, tc.want, ds.Flattened())
	}
}

func Test_setColSlice(t *testing.T) {
	type testCase struct {
		name    string
		newCol  [][]byte
		rowIdx  uint
		colIdx  uint
		want    [][]byte
		wantErr bool
	}
	testCases := []testCase{
		{
			name:    "overwrite the first col",
			newCol:  [][]byte{{5}, {6}},
			rowIdx:  0,
			colIdx:  0,
			want:    [][]byte{{5}, {2}, {6}, {4}},
			wantErr: false,
		},
		{
			name:    "overwrite the last col",
			newCol:  [][]byte{{5}, {6}},
			rowIdx:  0,
			colIdx:  1,
			want:    [][]byte{{1}, {5}, {3}, {6}},
			wantErr: false,
		},
		{
			name:    "returns an error if the new col has an invalid share size",
			newCol:  [][]byte{{5, 6}},
			rowIdx:  0,
			colIdx:  0,
			wantErr: true,
		},
		{
			name:    "returns an error if the new col would surpass the data square's width",
			newCol:  [][]byte{{5}, {6}},
			rowIdx:  1,
			colIdx:  0,
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		ds, err := newDataSquare([][]byte{{1}, {2}, {3}, {4}}, NewDefaultTree, 1)
		assert.NoError(t, err)
		err = ds.setColSlice(tc.colIdx, tc.rowIdx, tc.newCol)

		if tc.wantErr {
			assert.Error(t, err)
			return
		}
		assert.NoError(t, err)
		assert.Equal(t, tc.want, ds.Flattened())
	}
}

func BenchmarkEDSRootsWithDefaultTree(b *testing.B) {
	for i := 32; i < 513; i *= 2 {
		square, err := newDataSquare(genRandDS(i*2, int(shareSize)), NewDefaultTree, shareSize)
		if err != nil {
			b.Errorf("Failure to create square of size %d: %s", i, err)
		}
		b.Run(
			fmt.Sprintf("%dx%dx%d ODS", i, i, int(square.shareSize)),
			func(b *testing.B) {
				for b.Loop() {
					square.resetRoots()
					err := square.computeRoots()
					assert.NoError(b, err)
				}
			},
		)
	}
}

func BenchmarkEDSRootsWithErasuredNMT(b *testing.B) {
	maxSquareSize := 512

	for odsSize := 32; odsSize <= maxSquareSize; odsSize *= 2 {
		// number of shares in the extended data square's row/column
		edsSize := 2 * odsSize
		// generate an EDS with edsSize X edsSize dimensions in terms of shares.
		// the generated EDS does not conform to celestia-app specs in terms
		// of namespace version, also no erasure encoding takes place
		// yet none of these should impact the benchmarking
		ds := genRandSortedDS(edsSize, shareSize, defaultNamespaceIDSize)

		// a tree constructor for erasured nmt
		treeConstructor := newErasuredNamespacedMerkleTreeConstructor(uint64(edsSize),
			nmt.NamespaceIDSize(defaultNamespaceIDSize), nmt.IgnoreMaxNamespace(true),
			nmt.InitialCapacity(odsSize*2))

		square, err := newDataSquare(ds, treeConstructor, shareSize)
		if err != nil {
			b.Errorf("Failure to create square of size %d: %s", odsSize, err)
		}
		// the total size of the ODS in MiB
		odsSizeMiBytes := odsSize * odsSize * shareSize / mebibyte
		// the total size of the EDS in MiB
		edsSizeMiBytes := 4 * odsSizeMiBytes
		b.Run(
			fmt.Sprintf("%dx%dx%d ODS=%dMB, EDS=%dMB", odsSize, odsSize,
				int(square.shareSize),
				odsSizeMiBytes, edsSizeMiBytes),
			func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					square.resetRoots()
					err := square.computeRoots()
					assert.NoError(b, err)
				}
			},
		)
	}
}

func BenchmarkEDSRootsWithBufferedErasuredNMT(b *testing.B) {
	maxSquareSize := 512

	for odsSize := 32; odsSize <= maxSquareSize; odsSize *= 2 {
		// number of shares in the extended data square's row/column
		edsSize := 2 * odsSize
		// generate an EDS with edsSize X edsSize dimensions in terms of shares.
		// the generated EDS does not conform to celestia-app specs in terms
		// of namespace version, also no erasure encoding takes place
		// yet none of these should impact the benchmarking
		ds := genRandSortedDS(edsSize, shareSize, defaultNamespaceIDSize)

		// a tree constructor for erasured nmt
		parallelOps := runtime.NumCPU() * 4
		pool := newTreePool(uint(odsSize), parallelOps, nmt.NamespaceIDSize(defaultNamespaceIDSize), nmt.IgnoreMaxNamespace(true), nmt.InitialCapacity(odsSize*2))

		square, err := newDataSquare(ds, pool.NewConstructor(uint(odsSize)), shareSize)
		require.NoError(b, err)
		square.setParallelOps(parallelOps)
		// the total size of the ODS in MiB
		odsSizeMiBytes := odsSize * odsSize * shareSize / mebibyte
		// the total size of the EDS in MiB
		edsSizeMiBytes := 4 * odsSizeMiBytes
		b.Run(
			fmt.Sprintf("%dx%dx%d ODS=%dMB, EDS=%dMB", odsSize, odsSize,
				int(square.shareSize),
				odsSizeMiBytes, edsSizeMiBytes),
			func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					square.resetRoots()
					err := square.computeRoots()
					assert.NoError(b, err)
				}
			},
		)
	}
}

func computeRowProof(ds *dataSquare, rowIdx uint, colIdx uint) ([]byte, [][]byte, uint, uint, error) {
	tree := ds.createTreeFn(Row, rowIdx)
	data := ds.row(rowIdx)

	for i := uint(0); i < ds.width; i++ {
		err := tree.Push(data[i])
		if err != nil {
			return nil, nil, 0, 0, err
		}
	}

	merkleRoot, proof, proofIndex, numLeaves := treeProve(tree.(*DefaultTree), int(colIdx))
	return merkleRoot, proof, uint(proofIndex), uint(numLeaves), nil
}

func treeProve(d *DefaultTree, idx int) (merkleRoot []byte, proofSet [][]byte, proofIndex uint64, numLeaves uint64) {
	if err := d.SetIndex(uint64(idx)); err != nil {
		panic(fmt.Sprintf("don't call prove on a already used tree: %v", err))
	}
	for _, l := range d.leaves {
		d.Tree.Push(l)
	}
	return d.Prove()
}

type errorTree struct {
	*merkletree.Tree
	leaves [][]byte
}

func newErrorTree(_ Axis, _ uint) Tree {
	return &errorTree{
		Tree:   merkletree.New(sha256.New()),
		leaves: make([][]byte, 0, 128),
	}
}

func (d *errorTree) Push(data []byte) error {
	// ignore the idx, as this implementation doesn't need that info
	d.leaves = append(d.leaves, data)
	return nil
}

func (d *errorTree) Root() ([]byte, error) {
	return nil, fmt.Errorf("error")
}

func TestNmtVsBufferedNmtSameRoot(t *testing.T) {
	namespaceIDSizeOption := nmt.NamespaceIDSize(defaultNamespaceIDSize)
	sizes := []struct {
		name    string
		odsSize int
	}{
		{"ods-32", 32},
		{"ods-64", 64},
		{"ods-128", 128},
		{"ods-256", 256},
		{"ods-512", 512},
		// uneven sizes
		{"ods-35", 35},
		{"ods-67", 67},
	}

	t.Run("same-size-pool", func(t *testing.T) {
		for _, tc := range sizes {
			t.Run(tc.name, func(t *testing.T) {
				edsSize := tc.odsSize * 2
				data := genRandSortedDS(edsSize, shareSize, 8)

				pool := newTreePool(uint(tc.odsSize), 4, namespaceIDSizeOption, nmt.IgnoreMaxNamespace(true))
				constructor := newErasuredNamespacedMerkleTreeConstructor(uint64(tc.odsSize), namespaceIDSizeOption, nmt.IgnoreMaxNamespace(true))

				squareFast, err := newDataSquare(data, pool.NewConstructor(uint(tc.odsSize)), shareSize)
				require.NoError(t, err)
				squareFast.setParallelOps(4)
				squareRoot, err := newDataSquare(data, constructor, shareSize)
				require.NoError(t, err)

				rowRootsFast, err := squareFast.getRowRoots()
				require.NoError(t, err)
				rowRootsRoot, err := squareRoot.getRowRoots()
				require.NoError(t, err)

				colRootsFast, err := squareFast.getColRoots()
				require.NoError(t, err)
				colRootsRoot, err := squareRoot.getColRoots()
				require.NoError(t, err)

				require.Equal(t, rowRootsFast, rowRootsRoot, "row roots mismatch for ODS size %d", tc.odsSize)
				require.Equal(t, colRootsFast, colRootsRoot, "column roots mismatch for ODS size %d", tc.odsSize)
			})
		}
	})

	t.Run("pool-reallocation", func(t *testing.T) {
		// create a pool initialized with the smallest size
		pool := newTreePool(32, 4, namespaceIDSizeOption, nmt.IgnoreMaxNamespace(true))

		for _, tc := range sizes {
			t.Run(tc.name, func(t *testing.T) {
				edsSize := tc.odsSize * 2
				data := genRandSortedDS(edsSize, shareSize, 8)

				// use the same pool but with different square sizes to test reallocation
				squarePooled, err := newDataSquare(data, pool.NewConstructor(uint(tc.odsSize)), shareSize)
				require.NoError(t, err)
				squarePooled.setParallelOps(4)

				constructor := newErasuredNamespacedMerkleTreeConstructor(uint64(tc.odsSize), namespaceIDSizeOption, nmt.IgnoreMaxNamespace(true))
				squareRegular, err := newDataSquare(data, constructor, shareSize)
				require.NoError(t, err)

				rowRootsPooled, err := squarePooled.getRowRoots()
				require.NoError(t, err)
				colRootsPooled, err := squarePooled.getColRoots()
				require.NoError(t, err)

				rowRootsRegular, err := squareRegular.getRowRoots()
				require.NoError(t, err)
				colRootsRegular, err := squareRegular.getColRoots()
				require.NoError(t, err)

				require.Equal(t, rowRootsRegular, rowRootsPooled, "row roots mismatch for ODS size %d with pool reallocation", tc.odsSize)
				require.Equal(t, colRootsRegular, colRootsPooled, "column roots mismatch for ODS size %d with pool reallocation", tc.odsSize)
			})
		}
	})
}

func TestBufferedNMTParallelismComparison(t *testing.T) {
	t.Skip("Enable test to measure the best performance for certain num cpu multiplier for parallel rsmt2d computation")
	type benchmarkResult struct {
		Multiplier  float64
		ParallelOps int
		Duration    time.Duration
		Iterations  int
	}

	const (
		mebibyte   = 1024 * 1024
		iterations = 100
	)

	cpuMultipliers := []float64{1.0, 1.5, 2.0, 2.5, 3.0, 3.5, 4.0, 4.5, 5.0}
	squareSize := 512

	odsSize := squareSize
	edsSize := 2 * odsSize

	ds := genRandSortedDS(edsSize, shareSize, defaultNamespaceIDSize)

	odsSizeMiBytes := odsSize * odsSize * shareSize / mebibyte
	edsSizeMiBytes := 4 * odsSizeMiBytes

	results := make([]benchmarkResult, 0, len(cpuMultipliers))

	fmt.Printf("\n=== Running Benchmarks for 512x512 Square (ODS: %d MB, EDS: %d MB) ===\n", odsSizeMiBytes, edsSizeMiBytes)
	fmt.Printf("Each configuration: %d iterations\n\n", iterations)

	for _, multiplier := range cpuMultipliers {
		parallelOps := int(float64(runtime.NumCPU()) * multiplier)

		fmt.Printf("Testing CPU multiplier %.1fx (Parallel Ops: %d)...\n", multiplier, parallelOps)

		pool := newTreePool(
			uint(squareSize),
			parallelOps,
			nmt.NamespaceIDSize(defaultNamespaceIDSize),
			nmt.IgnoreMaxNamespace(true),
			nmt.InitialCapacity(odsSize*2),
		)

		square, err := newDataSquare(ds, pool.NewConstructor(uint(squareSize)), shareSize)
		require.NoError(t, err)
		square.setParallelOps(parallelOps)

		square.resetRoots()
		err = square.computeRoots()
		assert.NoError(t, err)

		start := time.Now()
		for i := 0; i < iterations; i++ {
			square.resetRoots()
			err := square.computeRoots()
			assert.NoError(t, err)
		}

		elapsed := time.Since(start)
		avgDuration := elapsed / time.Duration(iterations)

		results = append(results, benchmarkResult{
			Multiplier:  multiplier,
			ParallelOps: parallelOps,
			Duration:    avgDuration,
			Iterations:  iterations,
		})

		fmt.Printf("  Total: %.2f ms, Average: %.2f ms/op\n\n",
			float64(elapsed.Nanoseconds())/1_000_000, float64(avgDuration.Nanoseconds())/1_000_000)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Duration < results[j].Duration
	})

	fmt.Println("\n=== RESULTS SORTED BY PERFORMANCE (FASTEST FIRST) ===")
	fmt.Printf("\n%-5s %-10s %-12s %-15s\n",
		"Rank", "CPU Mult", "Parallel", "Time (ms)")
	fmt.Println(strings.Repeat("-", 45))

	for i, r := range results {
		msPerOp := float64(r.Duration.Nanoseconds()) / 1_000_000

		fmt.Printf("%-5d %-10.1f %-12d %-15.2f\n",
			i+1, r.Multiplier, r.ParallelOps, msPerOp)
	}
}

// setCell overwrites the contents of a specific cell. setCell does not perform
// any input validation so most use cases should use `SetCell` instead of
// `setCell`. This method exists strictly for testing.
func (ds *dataSquare) setCell(rowIdx uint, colIdx uint, newShare []byte) {
	ds.squareRow[rowIdx][colIdx] = newShare
	ds.squareCol[colIdx][rowIdx] = newShare
	ds.resetRoots()
}
