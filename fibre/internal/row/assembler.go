package row

import (
	"fmt"
	"sync"

	"github.com/klauspost/reedsolomon"
)

// Assembler assembles originalRows+parityRows row sets for Reed-Solomon
// encoding. It owns a [Pool] sized for the expected number of rows and a
// [Pool] for merkle trees.
//
// Original rows are a hybrid view over the input data: full middle
// rows alias data zero-copy, while two partial rows — row 0 (reserves
// firstRowOffset bytes for a header) and the truncated trailing row —
// come from a single pool slab alongside the parity rows.
//
// Assembler is safe for concurrent use.
type Assembler struct {
	originalRows   int
	parityRows     int
	treeBufferSize int

	rowsPool *Pool
	treePool *Pool
	// zeroRow is the shared, immutable all-zeros row that empty trailing
	// original rows alias. Sized to maxRowSize; aliased sub-slices are
	// used for each blob's rowSize.
	zeroRow []byte
}

// NewAssembler creates an Assembler for originalRows original rows and
// parityRows parity rows. It constructs internal [Pool]s sized for the
// assembler's slab shape (parityRows + partialRows) bounded by maxRowSize
// and a tree pool for per-blob Merkle-tree node storage sized by treeBufferSize.
func NewAssembler(originalRows, parityRows, maxRowSize, treeBufferSize int) (*Assembler, error) {
	if originalRows <= 0 || parityRows <= 0 {
		return nil, fmt.Errorf("originalRows=%d parityRows=%d must be positive", originalRows, parityRows)
	}
	return &Assembler{
		originalRows:   originalRows,
		parityRows:     parityRows,
		rowsPool:       NewPool(maxRowSize, parityRows+partialRows),
		treePool:       NewPool(treeBufferSize, 1),
		treeBufferSize: treeBufferSize,
		zeroRow:        reedsolomon.AllocAligned(1, maxRowSize)[0],
	}, nil
}

// Assemble returns an [Assembly] holding the originalRows+parityRows
// row view for encoding data at the given rowSize. Use [Assembly.Buffers]
// to access the rows; [Assembly.Free] releases the pooled storage.
//
// rows[:originalRows] are original rows: row 0 reserves firstRowOffset
// bytes for a header before its data, full middle rows alias data
// directly (zero-copy), a truncated trailing row is copied into a
// partial buffer, and any further empty rows share one zeroed row.
// rows[originalRows:] are zeroed parity rows from a pooled slab.
//
// The caller must not modify data or rows until [Assembly.Free] is called.
// Rows that alias data or the shared zero row are immutable.
func (a *Assembler) Assemble(data []byte, rowSize, firstRowOffset int) *Assembly {
	rows := make([][]byte, a.originalRows+a.parityRows)

	// pull one slab worth of buffers and zero them. Partial buffers must
	// be zeroed because we only write the data-backed portion below; the
	// parity rows because the encoder expects them clean on entry.
	pooled := a.rowsPool.Get(a.parityRows+partialRows, rowSize)
	for _, p := range pooled {
		clear(p)
	}

	// land pooled in one move: partial buffers at the originals tail
	// (overwritten as we fill below), parity in the parity range.
	copy(rows[a.originalRows-partialRows:], pooled)

	// row 0 reserves firstRowOffset bytes for a header, then the data
	// prefix. Backed by the first partial buffer (now at
	// rows[originalRows-partialRows]).
	rows[0] = rows[a.originalRows-partialRows]
	n := min(rowSize-firstRowOffset, len(data))
	copy(rows[0][firstRowOffset:], data[:n])

	// full middle rows alias data zero-copy.
	i := 1
	for i < a.originalRows && n+rowSize <= len(data) {
		rows[i] = data[n : n+rowSize]
		n += rowSize
		i++
	}

	// truncated trailing row: copy the remaining bytes into the second
	// partial buffer. Skipped when data fits perfectly (n == len(data))
	// or fills every original — in either case the second partial is
	// allocated but unused, returned to the pool on Free.
	if i < a.originalRows && n < len(data) {
		rows[i] = rows[a.originalRows-partialRows+1]
		copy(rows[i], data[n:])
		i++
	}

	// any remaining originals are empty — alias the shared zero row.
	for j := i; j < a.originalRows; j++ {
		rows[j] = a.zeroRow[:rowSize]
	}

	return &Assembly{
		asm:     a,
		slots:   pooled,
		treeBuf: a.treePool.GetRegion(1, a.treeBufferSize),
		rows:    rows,
	}
}

// Assembly owns pooled data and tree slabs produced by a single [Assembler.Assemble]
// call. Release is all-or-nothing via [Assembly.Free]; the slab is
// returned to the pool as one unit.
//
// Assembly is safe for concurrent use.
type Assembly struct {
	asm *Assembler

	mu      sync.RWMutex
	slots   [][]byte // [partial[0], partial[1], parity...]; nil after Free
	treeBuf []byte   // pooled Merkle-tree node storage; nil after Free
	rows    [][]byte // originalRows+parityRows assembled view; nil after Free
}

// Buffers returns the originalRows+parityRows assembled row view and the pooled
// Merkle-tree node storage for this blob. Both alias the Assembly's pooled
// storage and are valid until [Assembly.Free] (which nils them); callers must
// not use them concurrently with Free.
func (a *Assembly) Buffers() (rows [][]byte, tree []byte) {
	if a == nil {
		return nil, nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.rows, a.treeBuf
}

// Released reports whether [Assembly.Free] has run. After release the pooled
// storage has been returned to the pool and all rows are invalid.
func (a *Assembly) Released() bool {
	if a == nil {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.slots == nil
}

// Free returns the pooled row slab (parity + partial-row buffers) and the tree
// buffer to their pools. Subsequent calls are no-ops. Safe to call concurrently.
func (a *Assembly) Free() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.slots == nil {
		return
	}
	a.asm.rowsPool.Put(a.slots)
	a.asm.treePool.PutRegion(a.treeBuf)
	a.slots, a.treeBuf, a.rows = nil, nil, nil
}

// partialRows is the per-blob overhead beyond parity: two partial-row
// buffers (row 0 with the prefixed header, and the truncated trailing
// row) that the assembler owns rather than aliasing.
const partialRows = 2
