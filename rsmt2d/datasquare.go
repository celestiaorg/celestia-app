package rsmt2d

import (
	"errors"
	"fmt"
	"math"
	"sync"

	"golang.org/x/sync/errgroup"
)

// ErrUnevenChunks is thrown when non-nil shares are not all of equal size.
// Note: chunks is synonymous with shares.
var ErrUnevenChunks = errors.New("non-nil shares not all of equal size")

// dataSquare stores all data for an original data square (ODS) or extended
// data square (EDS). Pointers to the shares are stored in both row-major
// (squareRow) and column-major (squareCol) order. Both layouts reference the
// same underlying share byte slices (the share bytes themselves are not
// duplicated), which enables zero-allocation row and column slices.
type dataSquare struct {
	squareRow    [][][]byte // row-major
	squareCol    [][][]byte // col-major
	dataMutex    sync.Mutex
	width        uint
	shareSize    uint
	rowRoots     [][]byte
	colRoots     [][]byte
	createTreeFn TreeConstructorFn
	// parallelOps number of parallel operations when computing root
	parallelOps int
}

// getWidth returns the min width of the square which can fit the given data
func getWidth(data [][]byte) int {
	return int(math.Ceil(math.Sqrt(float64(len(data)))))
}

// newDataSquare populates the data square from the supplied data and treeCreator.
// No root calculation is performed.
// data may have nil values.
func newDataSquare(data [][]byte, treeCreator TreeConstructorFn, shareSize uint) (*dataSquare, error) {
	width := getWidth(data)
	if width*width != len(data) {
		// TODO: export this error and modify chunks to shares
		return nil, errors.New("number of chunks must be a square number")
	}

	for _, d := range data {
		if d != nil && len(d) != int(shareSize) {
			return nil, ErrUnevenChunks
		}
	}

	squareRow := make([][][]byte, width)
	for rowIdx := 0; rowIdx < width; rowIdx++ {
		squareRow[rowIdx] = data[rowIdx*width : rowIdx*width+width]

		for colIdx := 0; colIdx < width; colIdx++ {
			if squareRow[rowIdx][colIdx] != nil && len(squareRow[rowIdx][colIdx]) != int(shareSize) {
				return nil, ErrUnevenChunks
			}
		}
	}

	squareCol := make([][][]byte, width)
	for colIdx := 0; colIdx < width; colIdx++ {
		squareCol[colIdx] = make([][]byte, width)
		for rowIdx := 0; rowIdx < width; rowIdx++ {
			squareCol[colIdx][rowIdx] = data[rowIdx*width+colIdx]
		}
	}

	return &dataSquare{
		squareRow:    squareRow,
		squareCol:    squareCol,
		width:        uint(width),
		shareSize:    shareSize,
		createTreeFn: treeCreator,
	}, nil
}

// extendSquare extends the original data square by extendedWidth and fills
// the extended quadrants with fillerShare.
func (ds *dataSquare) extendSquare(extendedWidth uint, fillerShare []byte) error {
	if uint(len(fillerShare)) != ds.shareSize {
		// TODO: export this error and rename chunk to share
		return errors.New("filler chunk size does not match data square chunk size")
	}

	newWidth := ds.width + extendedWidth
	newSquareRow := make([][][]byte, newWidth)

	fillerExtendedRow := make([][]byte, extendedWidth)
	for i := uint(0); i < extendedWidth; i++ {
		fillerExtendedRow[i] = fillerShare
	}

	fillerRow := make([][]byte, newWidth)
	for i := uint(0); i < newWidth; i++ {
		fillerRow[i] = fillerShare
	}

	row := make([][]byte, ds.width)
	for i := uint(0); i < ds.width; i++ {
		copy(row, ds.squareRow[i])
		newSquareRow[i] = append(row, fillerExtendedRow...)
	}

	for i := ds.width; i < newWidth; i++ {
		newSquareRow[i] = make([][]byte, newWidth)
		copy(newSquareRow[i], fillerRow)
	}

	ds.squareRow = newSquareRow

	newSquareCol := make([][][]byte, newWidth)
	for colIdx := uint(0); colIdx < newWidth; colIdx++ {
		newSquareCol[colIdx] = make([][]byte, newWidth)
		for rowIdx := uint(0); rowIdx < newWidth; rowIdx++ {
			newSquareCol[colIdx][rowIdx] = newSquareRow[rowIdx][colIdx]
		}
	}
	ds.squareCol = newSquareCol
	ds.width = newWidth

	ds.resetRoots()

	return nil
}

func (ds *dataSquare) rowSlice(rowIdx uint, fromIdx uint, length uint) [][]byte {
	return ds.squareRow[rowIdx][fromIdx : fromIdx+length]
}

// row returns a row slice.
// Do not modify this slice directly, instead use SetCell.
func (ds *dataSquare) row(rowIdx uint) [][]byte {
	return ds.rowSlice(rowIdx, 0, ds.width)
}

func (ds *dataSquare) setRowSlice(rowIdx uint, fromIdx uint, newRow [][]byte) error {
	for i := uint(0); i < uint(len(newRow)); i++ {
		if len(newRow[i]) != int(ds.shareSize) {
			// TODO: export this error and rename chunk to share
			return errors.New("invalid chunk size")
		}
	}
	if fromIdx+uint(len(newRow)) > ds.width {
		return fmt.Errorf("cannot set row slice at (%d, %d) of length %d: because it would exceed the data square width %d", rowIdx, fromIdx, len(newRow), ds.width)
	}

	ds.dataMutex.Lock()
	defer ds.dataMutex.Unlock()

	for i := uint(0); i < uint(len(newRow)); i++ {
		ds.squareRow[rowIdx][fromIdx+i] = newRow[i]
		ds.squareCol[fromIdx+i][rowIdx] = newRow[i]
	}

	ds.resetRoots()

	return nil
}

func (ds *dataSquare) colSlice(rowIdx uint, colIdx uint, length uint) [][]byte {
	return ds.squareCol[colIdx][rowIdx : rowIdx+length]
}

// col returns a column slice.
// Do not modify this slice directly, instead use SetCell.
func (ds *dataSquare) col(colIdx uint) [][]byte {
	return ds.colSlice(0, colIdx, ds.width)
}

func (ds *dataSquare) setColSlice(colIdx uint, fromIdx uint, newCol [][]byte) error {
	for i := uint(0); i < uint(len(newCol)); i++ {
		if len(newCol[i]) != int(ds.shareSize) {
			// TODO: export this error and rename chunk to share
			return errors.New("invalid chunk size")
		}
	}
	if fromIdx+uint(len(newCol)) > ds.width {
		return fmt.Errorf("cannot set col slice at (%d, %d) of length %d: because it would exceed the data square width %d", fromIdx, colIdx, len(newCol), ds.width)
	}

	ds.dataMutex.Lock()
	defer ds.dataMutex.Unlock()

	for i := uint(0); i < uint(len(newCol)); i++ {
		ds.squareRow[fromIdx+i][colIdx] = newCol[i]
		ds.squareCol[colIdx][fromIdx+i] = newCol[i]
	}

	ds.resetRoots()

	return nil
}

func (ds *dataSquare) resetRoots() {
	// don't write nil if it's already nil
	// this prevents rewriting nil into shared memory slot
	// when resetRoots is used from multiple routines
	if ds.rowRoots != nil {
		ds.rowRoots = nil
	}
	if ds.colRoots != nil {
		ds.colRoots = nil
	}
}

// setParallelOps sets the number of parallel operations when computing the root,
// if the number is 0, the number of operations is not limited.
func (ds *dataSquare) setParallelOps(ops int) {
	ds.parallelOps = ops
}

func (ds *dataSquare) computeRoots() error {
	var g errgroup.Group

	rowRoots := make([][]byte, ds.width)
	colRoots := make([][]byte, ds.width)
	if ds.parallelOps != 0 {
		// this can be used in conjunction with reusing NMTs, we can reuse only a fixed number of NMTs
		// essentially having fixed allocations there when storing/computing hashes
		g.SetLimit(ds.parallelOps)
	}
	for i := uint(0); i < ds.width; i++ {
		i := i // https://go.dev/doc/faq#closures_and_goroutines
		g.Go(func() error {
			rowRoot, err := ds.getRowRoot(i)
			if err != nil {
				return err
			}
			rowRoots[i] = rowRoot
			return nil
		})

		g.Go(func() error {
			colRoot, err := ds.getColRoot(i)
			if err != nil {
				return err
			}
			colRoots[i] = colRoot
			return nil
		})
	}

	err := g.Wait()
	if err != nil {
		return err
	}

	ds.rowRoots = rowRoots
	ds.colRoots = colRoots
	return nil
}

// getRowRoots returns the Merkle roots of all the rows in the square.
func (ds *dataSquare) getRowRoots() ([][]byte, error) {
	if ds.rowRoots == nil {
		err := ds.computeRoots()
		if err != nil {
			return nil, err
		}
	}

	return ds.rowRoots, nil
}

// getRowRoot calculates and returns the root of the selected row. Note: unlike
// the getRowRoots method, getRowRoot does not write to the built-in cache.
// Returns an error if the row is incomplete (i.e. some shares are nil).
func (ds *dataSquare) getRowRoot(rowIdx uint) ([]byte, error) {
	if ds.rowRoots != nil {
		return ds.rowRoots[rowIdx], nil
	}

	tree := ds.createTreeFn(Row, rowIdx)
	row := ds.row(rowIdx)
	if !isComplete(row) {
		return nil, errors.New("can not compute root of incomplete row")
	}
	for _, d := range row {
		err := tree.Push(d)
		if err != nil {
			return nil, err
		}
	}

	return tree.Root()
}

// getColRoots returns the Merkle roots of all the columns in the square.
func (ds *dataSquare) getColRoots() ([][]byte, error) {
	if ds.colRoots == nil {
		err := ds.computeRoots()
		if err != nil {
			return nil, err
		}
	}

	return ds.colRoots, nil
}

// getColRoot calculates and returns the root of the selected column. Note: unlike
// the getColRoots method, getColRoot does not write to the built-in cache.
// Returns an error if the column is incomplete (i.e. some shares are nil).
func (ds *dataSquare) getColRoot(colIdx uint) ([]byte, error) {
	if ds.colRoots != nil {
		return ds.colRoots[colIdx], nil
	}

	tree := ds.createTreeFn(Col, colIdx)
	col := ds.col(colIdx)
	if !isComplete(col) {
		return nil, errors.New("can not compute root of incomplete column")
	}
	for _, d := range col {
		err := tree.Push(d)
		if err != nil {
			return nil, err
		}
	}

	return tree.Root()
}

// GetCell returns a copy of a specific cell.
func (ds *dataSquare) GetCell(rowIdx uint, colIdx uint) []byte {
	if ds.squareRow[rowIdx][colIdx] == nil {
		return nil
	}
	cell := make([]byte, ds.shareSize)
	copy(cell, ds.squareRow[rowIdx][colIdx])
	return cell
}

// SetCell sets a specific cell. The cell to set must be `nil`. Returns an error
// if the cell to set is not `nil` or newShare is not the correct size.
func (ds *dataSquare) SetCell(rowIdx uint, colIdx uint, newShare []byte) error {
	if ds.squareRow[rowIdx][colIdx] != nil {
		return fmt.Errorf("cannot set cell (%d, %d) as it already has a value %x", rowIdx, colIdx, ds.squareRow[rowIdx][colIdx])
	}
	if len(newShare) != int(ds.shareSize) {
		// TODO: export this error and rename chunk to share
		return fmt.Errorf("cannot set cell with chunk size %d because dataSquare chunk size is %d", len(newShare), ds.shareSize)
	}
	ds.squareRow[rowIdx][colIdx] = newShare
	ds.squareCol[colIdx][rowIdx] = newShare
	ds.resetRoots()
	return nil
}

// Flattened returns the concatenated rows of the data square.
func (ds *dataSquare) Flattened() [][]byte {
	flattened := make([][]byte, 0, ds.width*ds.width)
	for _, data := range ds.squareRow {
		flattened = append(flattened, data...)
	}

	return flattened
}

// isComplete returns true if all the shares are non-nil.
func isComplete(shares [][]byte) bool {
	for _, share := range shares {
		if share == nil {
			return false
		}
	}
	return true
}
