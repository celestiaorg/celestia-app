package row

import (
	"fmt"
	"sync"

	"github.com/klauspost/reedsolomon"
)

// Assembler assembles originalRows+parityRows row sets for Reed-Solomon
// encoding. It owns a [Pool] sized for its slab shape; callers don't
// need to know the exact row count it requests internally.
//
// Original rows are a hybrid view over the input data: full middle
// rows alias data zero-copy, while two partial rows — row 0 (reserves
// firstRowOffset bytes for a header) and the truncated trailing row —
// come from a single pool slab alongside the parity rows.
//
// Assembler is safe for concurrent use.
type Assembler struct {
	originalRows int
	parityRows   int
	pool         *Pool
	// zeroRow is the shared, immutable all-zeros row that empty trailing
	// original rows alias. Sized to maxRowSize; aliased sub-slices are
	// used for each blob's rowSize.
	zeroRow []byte
}

// NewAssembler creates an Assembler for originalRows original rows and
// parityRows parity rows. It constructs an internal [Pool] sized for
// the assembler's slab shape (parityRows + partialRows) and bounded by
// maxRowSize. maxRowSize must be a positive multiple of 64.
func NewAssembler(originalRows, parityRows, maxRowSize int) (*Assembler, error) {
	if originalRows <= 0 || parityRows <= 0 {
		return nil, fmt.Errorf("originalRows=%d parityRows=%d must be positive", originalRows, parityRows)
	}
	return &Assembler{
		originalRows: originalRows,
		parityRows:   parityRows,
		pool:         NewPool(maxRowSize, parityRows+partialRows),
		zeroRow:      reedsolomon.AllocAligned(1, maxRowSize)[0],
	}, nil
}

// Assemble returns an [Assembly] holding the originalRows+parityRows
// row view for encoding data at the given rowSize. Use [Assembly.Rows]
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
	pooled := a.pool.Get(a.parityRows+partialRows, rowSize)
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
		pool:  a.pool,
		slots: pooled,
		rows:  rows,
	}
}

// Assembly owns the pooled slab produced by a single [Assembler.Assemble]
// call. Release is all-or-nothing via [Assembly.Free]; the slab is
// returned to the pool as one unit.
//
// Assembly is safe for concurrent use.
type Assembly struct {
	mu    sync.RWMutex
	pool  *Pool
	slots [][]byte // [partial[0], partial[1], parity...]; nil after Free
	rows  [][]byte // originalRows+parityRows assembled view; nil after Free
}

// Rows returns the originalRows+parityRows assembled row view. Returns
// nil after [Assembly.Free].
//
// The returned slice shares its backing array with the Assembly; callers
// must not use it concurrently with Free, which nils the field. In practice
// Rows is intended for the encode phase before any Free has been issued.
func (a *Assembly) Rows() [][]byte {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.rows
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

// Free returns the pooled slab (parity + partial-row buffers) back to
// the pool. Subsequent calls are no-ops. Safe to call concurrently.
func (a *Assembly) Free() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.slots == nil {
		return
	}
	a.pool.Put(a.slots)
	a.slots, a.rows = nil, nil
}

// partialRows is the per-blob overhead beyond parity: two partial-row
// buffers (row 0 with the prefixed header, and the truncated trailing
// row) that the assembler owns rather than aliasing.
const partialRows = 2
