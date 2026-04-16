package fibre

import (
	"fmt"
	"sync"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d"
	"github.com/klauspost/reedsolomon"
)

// RowAssembler assembles K+N row sets for encoding and pools backing storage
// for parity rows: original rows are a hybrid view over the input
// data (zero-copy where possible), parity rows come from pooled storage.
//
// Two pooling strategies are used. When rowSize == MaxRowSize, entire parity
// batches are pooled (bounded by MaxRowPoolCap). For smaller rows, parity is
// packed into fixed-size chunks via sync.Pool.
//
// RowAssembler is safe for concurrent use.
type RowAssembler struct {
	codec *rsema1d.Config
	cfg   *RowAssemblerConfig

	maxPool   *maxRowPool
	chunkPool sync.Pool // []byte of PackedChunkSize
	metaPool  sync.Pool // *meta

	zeroRow []byte // read-only; shared across concurrent Assemble calls
}

// RowAssemblerConfig contains tuning knobs for RowAssembler.
// K and N are taken from the [rsema1d.Config] passed to [NewRowAssembler].
type RowAssemblerConfig struct {
	// MaxRowSize is the largest row size the allocator must support.
	MaxRowSize int

	// PackedChunkSize is the backing chunk size used when rowSize < MaxRowSize.
	// Smaller rows are packed into chunks. Larger values reduce pool.Get/Put
	// traffic for small and medium rows, at the cost of larger retained objects
	// and potential tail slack for row sizes that do not divide the chunk size evenly.
	PackedChunkSize int

	// MaxRowPoolCap is the maximum number of idle whole-batch allocations
	// to retain for rowSize == MaxRowSize. This bounds idle retained memory for
	// the largest-row path, but does not limit how many max-row batches may be
	// checked out concurrently. A value of 0 disables idle retention for that path.
	MaxRowPoolCap int
}

// DefaultRowAssemblerConfig returns a RowAssemblerConfig initialized to
// conservative defaults suitable for typical celestia-app sizing.
func DefaultRowAssemblerConfig() *RowAssemblerConfig {
	return &RowAssemblerConfig{
		MaxRowSize:      32832,
		PackedChunkSize: 1 << 19, // 512 KiB is enough for parity-only chunking while keeping retained chunks smaller.
		MaxRowPoolCap:   6,
	}
}

// Validate checks RowAssemblerConfig constraints.
func (c *RowAssemblerConfig) Validate() error {
	if c.MaxRowSize < 64 {
		return fmt.Errorf("MaxRowSize must be at least 64, got %d", c.MaxRowSize)
	}
	if c.MaxRowSize%64 != 0 {
		return fmt.Errorf("MaxRowSize must be a multiple of 64, got %d", c.MaxRowSize)
	}
	if c.PackedChunkSize < 64 {
		return fmt.Errorf("PackedChunkSize must be at least 64, got %d", c.PackedChunkSize)
	}
	if c.PackedChunkSize%64 != 0 {
		return fmt.Errorf("PackedChunkSize must be a multiple of 64, got %d", c.PackedChunkSize)
	}
	if c.PackedChunkSize < 2*c.MaxRowSize {
		return fmt.Errorf("PackedChunkSize (%d) must be >= 2*MaxRowSize (%d)", c.PackedChunkSize, 2*c.MaxRowSize)
	}
	if c.MaxRowPoolCap < 0 {
		return fmt.Errorf("MaxRowPoolCap must be non-negative, got %d", c.MaxRowPoolCap)
	}
	return nil
}

// NewRowAssembler creates a RowAssembler. K and N are taken from the codec
// [rsema1d.Config]; pooling behaviour is controlled by [RowAssemblerConfig].
func NewRowAssembler(codec *rsema1d.Config, cfg *RowAssemblerConfig) (*RowAssembler, error) {
	if err := codec.Validate(); err != nil {
		return nil, fmt.Errorf("invalid codec config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid row allocator config: %w", err)
	}

	totalRows := codec.K + codec.N
	a := &RowAssembler{
		codec: codec,
		cfg:   cfg,
		maxPool: &maxRowPool{
			free: make([][][]byte, 0, cfg.MaxRowPoolCap),
			cap:  cfg.MaxRowPoolCap,
		},
		zeroRow: reedsolomon.AllocAligned(1, cfg.MaxRowSize)[0],
	}
	a.chunkPool.New = func() any {
		return reedsolomon.AllocAligned(1, cfg.PackedChunkSize)[0]
	}
	a.metaPool.New = func() any {
		return &meta{
			rows:   make([][]byte, totalRows),
			chunks: make([][]byte, maxChunks(cfg, codec.N)),
		}
	}
	return a, nil
}

// Assemble returns K+N rows for encoding data with the given rowSize.
//
// rows[:K] are original rows: row 0 reserves firstRowDataOffset bytes,
// full middle rows alias data directly (zero-copy), a partial tail gets
// a tail buffer, and empty trailing rows share one zeroed row.
// rows[K:] are zeroed parity rows from pooled storage.
//
// Callers must not modify data after Assemble. The returned rows must also be
// treated as immutable until release, because some rows may alias the input
// data or shared zero-filled backing storage. Call release when done.
func (a *RowAssembler) Assemble(data []byte, rowSize, firstRowDataOffset int) (rows [][]byte, release func()) {
	m := a.metaPool.Get().(*meta)
	rows = m.rows[:a.codec.K+a.codec.N]

	if rowSize == a.cfg.MaxRowSize {
		return a.assembleMax(m, rows, data, rowSize, firstRowDataOffset)
	}

	// packed path: parity rows + head/tail carved from pooled chunks.
	head, tail := a.packParity(rows[a.codec.K:], m.chunks, rowSize)
	a.fillData(rows[:a.codec.K], data, rowSize, firstRowDataOffset, head, tail)

	release = func() {
		for _, ch := range m.chunks {
			if ch == nil {
				break
			}
			a.chunkPool.Put(ch) //nolint:staticcheck // SA6002: []byte is intentional; wrapping adds complexity for no benefit on the release path
		}
		clear(m.rows)
		clear(m.chunks)
		a.metaPool.Put(m)
	}
	return rows, release
}

// assembleMax handles the MaxRowSize path where entire parity batches are
// pooled as one unit instead of being packed into chunks. Head and tail
// for fillOriginal are allocated as extra rows in the same batch.
func (a *RowAssembler) assembleMax(m *meta, rows [][]byte, data []byte, rowSize, firstRowDataOffset int) ([][]byte, func()) {
	batch := a.maxPool.Get(a.codec.N+extraRows, a.cfg.MaxRowSize)
	copy(rows[a.codec.K:], batch[:a.codec.N])
	for _, row := range rows[a.codec.K:] {
		clear(row)
	}
	a.fillData(rows[:a.codec.K], data, rowSize, firstRowDataOffset, batch[a.codec.N], batch[a.codec.N+1])

	release := func() {
		a.maxPool.Put(batch)
		clear(m.rows)
		a.metaPool.Put(m)
	}
	return rows, release
}

// extraRows is the number of extra rows beyond parity (head + tail).
const extraRows = 2

// packParity packs zeroed parity rows into pooled chunks and returns head
// and tail buffers carved from the last chunk's tail.
func (a *RowAssembler) packParity(rows [][]byte, chunks [][]byte, rowSize int) (head, tail []byte) {
	rowsPerChunk := a.cfg.PackedChunkSize / rowSize
	numChunks := (len(rows) + extraRows + rowsPerChunk - 1) / rowsPerChunk
	clear(chunks[numChunks:])

	ri, end := 0, 0
	for i := range numChunks {
		chunk := a.chunkPool.Get().([]byte)
		chunks[i] = chunk
		end = 0
		for j := 0; j < rowsPerChunk && ri < len(rows); j++ {
			off := j * rowSize
			rows[ri] = chunk[off : off+rowSize]
			end = off + rowSize
			ri++
		}
	}
	for _, row := range rows {
		clear(row)
	}
	return chunks[numChunks-1][end : end+rowSize], chunks[numChunks-1][end+rowSize : end+2*rowSize]
}

// fillData builds original rows as a hybrid view over data. Head is used for
// rows[0] (prefix offset + start of data). Tail is placed at the partial
// trailing row position if needed. Full middle rows alias data directly.
// Empty trailing rows share a read-only zero row.
func (a *RowAssembler) fillData(rows [][]byte, data []byte, rowSize, offset int, head, tail []byte) {
	// head row: prefix offset + start of data.
	rows[0] = head
	clear(head)
	n := min(rowSize-offset, len(data))
	copy(head[offset:], data[:n])

	// full middle rows alias data directly (zero-copy).
	ri := 1
	for ri < len(rows) && n+rowSize <= len(data) {
		rows[ri] = data[n : n+rowSize]
		n += rowSize
		ri++
	}

	// partial tail row.
	if ri < len(rows) && n < len(data) {
		rows[ri] = tail
		clear(tail)
		copy(tail, data[n:])
		ri++
	}

	// empty original rows share a read-only zero row.
	if ri < len(rows) {
		zero := a.zeroRow[:rowSize]
		for i := ri; i < len(rows); i++ {
			rows[i] = zero
		}
	}
}

// maxChunks returns the maximum number of packed chunks needed for any
// row size that hits the packed path, including head and tail slots.
func maxChunks(cfg *RowAssemblerConfig, parityRows int) int {
	minRowsPerChunk := cfg.PackedChunkSize / cfg.MaxRowSize
	return (parityRows + extraRows + minRowsPerChunk - 1) / minRowsPerChunk
}

type meta struct {
	rows   [][]byte
	chunks [][]byte
}

// maxRowPool bounds idle retention for the MaxRowSize batch path.
type maxRowPool struct {
	mu   sync.Mutex
	free [][][]byte
	cap  int
}

func (p *maxRowPool) Get(n, rowSize int) [][]byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.free) == 0 {
		return reedsolomon.AllocAligned(n, rowSize)
	}
	last := len(p.free) - 1
	rows := p.free[last]
	p.free[last] = nil
	p.free = p.free[:last]
	return rows
}

func (p *maxRowPool) Put(rows [][]byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.free) < p.cap {
		p.free = append(p.free, rows)
	}
}
