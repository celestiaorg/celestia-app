package row

import (
	"fmt"
	"sync"

	"github.com/klauspost/reedsolomon"
)

// Assembler assembles originalRows+parityRows row sets for Reed-Solomon
// encoding. It owns a [Pool] sized for its batch shape; callers don't
// need to know the exact row count it requests internally.
//
// Original rows are a hybrid view over the input data (zero-copy where
// possible); parity rows plus head and tail come from a single pool
// batch.
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
// parityRows parity rows. It constructs an internal [Pool] sized for the
// assembler's batch shape (parityRows + head + tail) and bounded by
// maxRowSize. maxRowSize must be a positive multiple of 64.
func NewAssembler(originalRows, parityRows, maxRowSize int) (*Assembler, error) {
	if originalRows <= 0 || parityRows <= 0 {
		return nil, fmt.Errorf("originalRows=%d parityRows=%d must be positive", originalRows, parityRows)
	}
	return &Assembler{
		originalRows: originalRows,
		parityRows:   parityRows,
		pool:         NewPool(maxRowSize, parityRows+extraRows),
		zeroRow:      reedsolomon.AllocAligned(1, maxRowSize)[0],
	}, nil
}

// Assemble returns an [Assembly] holding the originalRows+parityRows
// row view for encoding data at the given rowSize. Use [Assembly.Rows]
// to access the rows; [Assembly.Free] releases the pooled storage.
//
// rows[:originalRows] are original rows: row 0 reserves firstRowOffset
// bytes for a blob header, full middle rows alias data directly (zero-copy),
// a partial tail row is copied to a pooled buffer, and empty trailing
// rows share one zeroed row.
// rows[originalRows:] are zeroed parity rows from a pooled batch.
//
// The caller must not modify data or rows until [Assembly.Free] is called.
// Rows that alias data or the shared zero row are immutable.
func (a *Assembler) Assemble(data []byte, rowSize, firstRowOffset int) *Assembly {
	rows := make([][]byte, a.originalRows+a.parityRows)

	pooled := a.pool.Get(a.parityRows+extraRows, rowSize)
	for i := range a.parityRows {
		rows[a.originalRows+i] = pooled[i]
		clear(pooled[i])
	}
	// zero head/tail too: fillOriginal only writes the portions backed
	// by data, so bytes past the written region would carry stale
	// content from a prior use of the pooled batch.
	head, tail := pooled[a.parityRows], pooled[a.parityRows+1]
	clear(head)
	clear(tail)
	a.fillOriginal(rows[:a.originalRows], data, rowSize, firstRowOffset, head, tail, a.zeroRow[:rowSize])

	return &Assembly{
		pool:  a.pool,
		slots: pooled,
		rows:  rows,
	}
}

// fillOriginal populates original rows as a hybrid view over data.
// head hosts row 0 (prefix offset + data start); tail hosts any partial
// trailing row; full middle rows alias data directly; empty rows share
// the zero row.
func (a *Assembler) fillOriginal(rows [][]byte, data []byte, rowSize, offset int, head, tail, zero []byte) {
	rows[0] = head
	n := min(rowSize-offset, len(data))
	copy(head[offset:], data[:n])

	i := 1
	for i < len(rows) && n+rowSize <= len(data) {
		rows[i] = data[n : n+rowSize]
		n += rowSize
		i++
	}

	if i < len(rows) && n < len(data) {
		rows[i] = tail
		copy(tail, data[n:])
		i++
	}

	for j := i; j < len(rows); j++ {
		rows[j] = zero
	}
}

// Assembly owns the pooled batch produced by a single [Assembler.Assemble]
// call. Release is all-or-nothing via [Assembly.Free]; the batch is
// returned to the pool as one unit.
//
// Assembly is safe for concurrent use.
type Assembly struct {
	mu    sync.RWMutex
	pool  *Pool
	slots [][]byte // [parity..., head, tail]; nil after Free
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

// Free returns the pooled batch (parity + head + tail) back to the pool.
// Subsequent calls are no-ops. Safe to call concurrently.
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

// extraRows is the per-blob overhead beyond parity: head + tail.
const extraRows = 2
