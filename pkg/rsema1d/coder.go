package rsema1d

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d/merkle"
	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d/rlc"
	"github.com/klauspost/reedsolomon"
)

// Coder provides encoding and reconstruction operations with a cached Reed-Solomon encoder.
// Use NewCoder to create an instance and reuse it for multiple operations with the same Config.
type Coder struct {
	config *Config
	enc    reedsolomon.Encoder
	busy   atomic.Int32 // worker slots reserved by in-flight encodes
}

// NewCoder creates a Coder with cached Reed-Solomon encoder.
// Optional reedsolomon.Option values are forwarded to the underlying encoder
// (e.g., reedsolomon.WithWorkAllocator to control work buffer allocation).
func NewCoder(cfg *Config, opts ...reedsolomon.Option) (*Coder, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	rsOpts := append([]reedsolomon.Option{reedsolomon.WithLeopardGF16(true)}, opts...)
	enc, err := reedsolomon.New(cfg.K, cfg.N, rsOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create encoder: %w", err)
	}

	return &Coder{config: cfg, enc: enc}, nil
}

// Encode creates parity and commitment for K+N rows, allocating storage for the
// Merkle trees. rows must have length K+N. Original data goes in rows[:K];
// rows[K:] must be allocated at the row size and are overwritten with parity.
func (c *Coder) Encode(rows [][]byte) (*ExtendedData, error) {
	return c.EncodeWithTree(rows, nil)
}

// EncodeWithTree is like [Coder.Encode] but builds the row and RLC Merkle trees
// into the caller-provided treeBuffer instead of allocating. treeBuffer must be
// at least [Config.TreeBufferSize] bytes — the tree panics if it is too small.
// The returned [ExtendedData]'s trees alias treeBuffer, which must outlive them
// and not be reused meanwhile.
func (c *Coder) EncodeWithTree(rows [][]byte, treeBuffer []byte) (*ExtendedData, error) {
	if err := c.validateRows(rows); err != nil {
		return nil, err
	}
	if err := c.encodeParity(rows); err != nil {
		return nil, fmt.Errorf("failed to encode: %w", err)
	}
	return c.commit(rows, treeBuffer), nil
}

const (
	maxSplitParts = 16
	minSplitBytes = 2 << 10
)

// encodeParity runs Reed-Solomon over column ranges in parallel. Columns are
// independent, so the parity equals a single Encode.
func (c *Coder) encodeParity(rows [][]byte) error {
	parts := c.reserveParts(len(rows[0]))
	defer c.busy.Add(-int32(parts))
	if parts == 1 {
		return c.enc.Encode(rows)
	}
	size := len(rows[0]) / parts
	// One slab of row views shared by all parts; each part owns a disjoint window.
	subs := make([][]byte, parts*len(rows))
	errs := make([]error, parts)
	var wg sync.WaitGroup
	for p := range parts {
		wg.Go(func() {
			sub := subs[p*len(rows) : (p+1)*len(rows)]
			for i, r := range rows {
				sub[i] = r[p*size : (p+1)*size]
			}
			errs[p] = c.enc.Encode(sub)
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}

// reserveParts reserves worker slots for one encode and returns how many parts
// to split it into. Slots held by other encodes are not available, so the total
// across concurrent encodes stays within WorkerCount, except that every encode
// always gets one slot to run on its own goroutine.
func (c *Coder) reserveParts(rowSize int) int {
	for {
		busy := c.busy.Load()
		parts := splitParts(rowSize, c.config.WorkerCount-int(busy))
		if c.busy.CompareAndSwap(busy, busy+int32(parts)) {
			return parts
		}
	}
}

// splitParts returns the power-of-two number of column ranges to encode a row
// of rowSize bytes in, using at most workers goroutines. Each range stays a
// multiple of the Leopard chunk and at least minSplitBytes long.
func splitParts(rowSize, workers int) int {
	if rowSize < minSplitRowBytes {
		return 1
	}
	parts := 1
	for next := 2; next <= min(workers, maxSplitParts); next *= 2 {
		if rowSize%(next*field.LeopardChunkSize) != 0 || rowSize/next < minSplitBytes {
			break
		}
		parts = next
	}
	return parts
}

func (c *Coder) validateRows(rows [][]byte) error {
	if len(rows) != c.config.K+c.config.N {
		return fmt.Errorf("expected %d rows, got %d", c.config.K+c.config.N, len(rows))
	}
	// encodeParity slices every row to rows[0]'s length, so an unequal row
	// would panic inside a worker goroutine; reject it here instead.
	for i, r := range rows {
		if len(r) != len(rows[0]) {
			return fmt.Errorf("row %d has length %d, want %d", i, len(r), len(rows[0]))
		}
	}
	return nil
}

// commit builds the row and RLC Merkle trees and the commitment over them. buf
// backs both trees' nodes (row tree first, RLC tree in the tail); if nil it is
// allocated. A too-small buf panics inside the tree builder.
func (c *Coder) commit(extendedRows [][]byte, buf []byte) *ExtendedData {
	rowSize := merkle.TreeBufferSize(c.config.K + c.config.N)
	if buf == nil {
		buf = make([]byte, rowSize+merkle.TreeBufferSize(c.config.K))
	}

	// build Merkle tree over the extended rows (uses buf[:rowSize]; the tree
	// builder panics if buf is too small)
	rowTree := buildRowTree(extendedRows, c.config, buf)
	rowRoot := rowTree.Root()

	// derive RLC coefficients and compute RLC results for original rows.
	coeffs := rlc.DeriveCoefficients(rowRoot, c.config.K, c.config.N, len(extendedRows[0]), c.config.WorkerCount)
	rlcVec := rlc.Compute(extendedRows[:c.config.K], coeffs, c.config.WorkerCount)

	// build Merkle tree over the RLC values from the buffer's tail
	rlcTree := buildRLCTree(rlcVec, c.config, buf[rowSize:])
	rlcRoot := rlcTree.Root()

	// create commitment: SHA256(rowRoot || rlcRoot)
	h := sha256.New()
	h.Write(rowRoot[:])
	h.Write(rlcRoot[:])
	var commitment Commitment
	h.Sum(commitment[:0])

	return &ExtendedData{
		config:     c.config,
		rows:       extendedRows,
		rlc:        rlcVec,
		commitment: commitment,
		rowsTree:   rowTree,
	}
}

// buildRowTree creates the Merkle tree over the K+N extended rows, which are
// already materialized.
func buildRowTree(extended [][]byte, config *Config, buf []byte) *merkle.Tree {
	nodes := buf[:merkle.TreeBufferSize(config.K+config.N)]
	return merkle.NewTree(nodes, extended, config.WorkerCount)
}

// buildRLCTree creates the Merkle tree over the K original RLC values.
func buildRLCTree(rlc rlc.Vector, config *Config, buf []byte) *merkle.Tree {
	nodes := buf[:merkle.TreeBufferSize(config.K)]
	return merkle.NewTree(nodes, newRLCLeaves(config.K).set(rlc), config.WorkerCount)
}

// rlcLeaves holds RLC values serialized as Merkle leaves: one flat buffer and
// a view per value into it, reusable across calls.
type rlcLeaves struct {
	buf   []byte
	views [][]byte
}

func newRLCLeaves(k int) *rlcLeaves {
	buf := make([]byte, k*field.GF128Size)
	views := make([][]byte, k)
	for i := range views {
		views[i] = buf[i*field.GF128Size : (i+1)*field.GF128Size]
	}
	return &rlcLeaves{buf: buf, views: views}
}

// set serializes v, which must hold exactly as many values as l was built for,
// and returns the leaves.
func (l *rlcLeaves) set(v rlc.Vector) [][]byte {
	rlc.Encode(l.buf, v)
	return l.views
}
