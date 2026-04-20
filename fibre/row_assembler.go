package fibre

import (
	"fmt"
	"sync"

	"github.com/celestiaorg/celestia-app/v9/fibre/internal/slab"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d"
	"github.com/klauspost/reedsolomon"
)

// RowAssembler assembles K+N row sets for Reed-Solomon encoding.
//
// Original rows are a hybrid view over the input data (zero-copy where
// possible); parity rows come from a dedicated slab-backed parity pool, while
// the RS encoder draws its large scratch buffers from a separate work pool via
// [RowAssembler.WorkAllocator]. The pools are intentionally split: sharing one
// free-list across these two allocation classes caused severe external
// fragmentation and slab churn under concurrent uploads.
//
// The pools grow on demand when concurrent encodings exceed the initial
// capacity and shrink back when excess slabs become fully free.
//
// RowAssembler is safe for concurrent use.
type RowAssembler struct {
	codec *rsema1d.Config

	// parityPool backs Assemble (parity rows + head + tail). Slabs here are
	// sized around N*rowSize; keeping them in a dedicated pool prevents the
	// much larger work requests from fragmenting them.
	parityPool *slab.Pool

	// workPool backs [RowAssembler.WorkAllocator]. The RS encoder asks for
	// one large contiguous scratch region per encode; keeping work in its
	// own pool means parity allocations never carve holes in a work slab.
	workPool *slab.Pool

	zeroRow []byte
}

// NewRowAssembler creates a RowAssembler backed by slab pools with rows
// up to maxRowSize bytes.
//
// The returned assembler's work pool also provides buffers for the RS
// encoder via [RowAssembler.WorkAllocator].
//
// Large slabs are allocated off-heap via mmap and are only released when they
// become fully free and [Pool.Shrink] drops them. There is currently no
// explicit Close/Destroy API, so callers should treat RowAssembler as a
// long-lived object and reuse it rather than constructing one per blob.
func NewRowAssembler(codec *rsema1d.Config, maxRowSize int) (*RowAssembler, error) {
	if err := codec.Validate(); err != nil {
		return nil, fmt.Errorf("invalid codec config: %w", err)
	}
	if maxRowSize < 64 || maxRowSize%64 != 0 {
		return nil, fmt.Errorf("maxRowSize must be a positive multiple of 64, got %d", maxRowSize)
	}

	return &RowAssembler{
		codec:      codec,
		parityPool: slab.New(),
		workPool:   slab.New(),
		zeroRow:    reedsolomon.AllocAligned(1, maxRowSize)[0],
	}, nil
}

// WorkAllocator returns a [reedsolomon.WorkAllocator] backed by the
// assembler's work pool. Pass it to [reedsolomon.WithWorkAllocator] when
// constructing the RS encoder.
func (a *RowAssembler) WorkAllocator() reedsolomon.WorkAllocator {
	return a.workPool
}

// Assemble returns K+N rows for encoding data with the given rowSize.
//
// rows[:K] are original rows: row 0 reserves firstRowOffset bytes for a
// header, full middle rows alias data directly (zero-copy), a partial tail
// row is copied to a pooled buffer, and empty trailing rows share one
// zeroed row.
// rows[K:] are zeroed parity rows from pooled storage.
//
// The caller must not modify data or rows until [Assembly.Free] is called.
// Rows that alias data or the shared zero row are immutable.
func (a *RowAssembler) Assemble(data []byte, rowSize, firstRowOffset int) (rows [][]byte, asm *Assembly) {
	rows = make([][]byte, a.codec.K+a.codec.N)

	// n parity rows + head + tail, all from one slab.
	pooled := a.parityPool.Get(a.codec.N+extraRows, rowSize)
	for i := range a.codec.N {
		rows[a.codec.K+i] = pooled[i]
		clear(pooled[i])
	}
	head, tail := pooled[a.codec.N], pooled[a.codec.N+1]
	a.fillOriginal(rows[:a.codec.K], data, rowSize, firstRowOffset, head, tail)

	return rows, &Assembly{
		pool:     a.parityPool,
		workPool: a.workPool,
		slots:    pooled,
		rows:     rows,
		k:        a.codec.K,
	}
}

// fillOriginal populates original rows as a hybrid view over data.
// head hosts row 0 (prefix offset + data start); tail hosts any partial
// trailing row; full middle rows alias data directly; empty rows share a
// read-only zero row.
func (a *RowAssembler) fillOriginal(rows [][]byte, data []byte, rowSize, offset int, head, tail []byte) {
	rows[0] = head
	clear(head)
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
		clear(tail)
		copy(tail, data[n:])
		i++
	}

	if i < len(rows) {
		zero := a.zeroRow[:rowSize]
		for j := i; j < len(rows); j++ {
			rows[j] = zero
		}
	}
}

// extraRows is the pooled slot count beyond parity: head + tail.
const extraRows = 2

// Assembly owns the pooled row storage produced by a single
// [RowAssembler.Assemble] call. Callers incrementally release parity rows via
// [Assembly.Free] with global blob indices, and finalize with a nil argument
// to return any remaining rows to the parity pool.
//
// Assembly is safe for concurrent use. The internal lock is a RWMutex so
// that the hot read path (Blob.Row → Freed) can proceed in parallel across
// validator goroutines; Free takes the exclusive side briefly to mutate
// slots/rows.
type Assembly struct {
	mu       sync.RWMutex
	pool     *slab.Pool // parity pool; owns slots
	workPool *slab.Pool // work pool; shrunk alongside parity on terminal release
	slots    [][]byte   // [N parity..., head, tail]; nil entries mean freed
	rows     [][]byte   // K+N assembled view; nil'd per-row on partial Free, whole slice on terminal Free
	k        int        // K; translates global blob indices to parity offsets
}

// Rows returns the K+N assembled row view. Returns nil after terminal Free.
// Entries for rows released via partial Free are set to nil — the view
// always reflects currently-live row data without exposing stale pool memory.
//
// The returned slice shares its backing array with the Assembly; callers
// must not use it concurrently with Free, which mutates entries. In practice
// Rows is intended for the encode phase before any Free has been issued.
func (a *Assembly) Rows() [][]byte {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.rows
}

// Freed reports whether the pooled slot for the given global blob row index
// has been freed. Terminally-freed Assemblies (after Free(nil)) report true
// for every index.
//
// Original rows (rowIndex < K) only become freed on terminal release, since
// partial frees only affect parity slots. Parity rows (>= K) are freed
// individually.
//
// Synchronized internally; safe to call concurrently with Free.
func (a *Assembly) Freed(rowIndex int) bool {
	if a == nil {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.slots == nil {
		return true
	}
	parity := len(a.slots) - extraRows
	off := rowIndex - a.k
	if off < 0 || off >= parity {
		return false
	}
	return a.slots[off] == nil
}

// Free releases pooled row storage.
//
// When rowIndices is non-nil, parity rows at the given global blob indices
// (>= K) are freed back to the slab pool for immediate reuse; indices < K
// and already-freed entries are ignored. The Assembly stays alive for
// further calls and [Freed] reflects the updated state.
//
// When rowIndices is nil, all remaining rows (including head and tail) are
// freed and the row-header slice is returned to the assembler's cache;
// subsequent calls are no-ops.
//
// Safe to call concurrently.
func (a *Assembly) Free(rowIndices []int) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.slots == nil {
		return
	}
	if rowIndices == nil {
		a.freeAll()
	} else {
		a.freeSubset(rowIndices)
	}
}

// freeAll returns every remaining pooled buffer to the slab pool and
// triggers a lifecycle-boundary Shrink on both pools. Caller must hold
// a.mu and have verified a.slots != nil.
func (a *Assembly) freeAll() {
	toFree := make([][]byte, 0, len(a.slots))
	for _, b := range a.slots {
		if b != nil {
			toFree = append(toFree, b)
		}
	}
	if len(toFree) > 0 {
		a.pool.Put(toFree)
	}
	// shrink at blob lifecycle boundary: release excess slabs that
	// accumulated during concurrent uploads. Work slabs already came back
	// to workPool when RS encoding finished, so shrinking it here picks up
	// any burst-era slabs that are no longer needed.
	a.pool.Shrink()
	a.workPool.Shrink()
	a.slots = nil
	a.rows = nil
}

// freeSubset returns parity rows at the given global blob indices (>= K)
// to the slab pool and nils out their entries in the header slice so
// Rows() never hands out stale pool memory. Indices < K and already-freed
// entries are ignored. Caller must hold a.mu and have verified
// a.slots != nil.
func (a *Assembly) freeSubset(rowIndices []int) {
	parity := len(a.slots) - extraRows
	toFree := make([][]byte, 0, len(rowIndices))
	for _, idx := range rowIndices {
		off := idx - a.k
		if off >= 0 && off < parity && a.slots[off] != nil {
			toFree = append(toFree, a.slots[off])
			a.slots[off] = nil
			a.rows[idx] = nil
		}
	}
	if len(toFree) > 0 {
		a.pool.Put(toFree)
	}
}
