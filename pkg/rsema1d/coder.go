package rsema1d

import (
	"crypto/sha256"
	"fmt"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/merkle"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/rlc"
	"github.com/klauspost/reedsolomon"
)

// Coder provides encoding and reconstruction operations with a cached Reed-Solomon encoder.
// Use NewCoder to create an instance and reuse it for multiple operations with the same Config.
type Coder struct {
	config *Config
	enc    reedsolomon.Encoder
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
// Merkle trees. rows must have length K+N. Original data goes in rows[:K], and
// parity rows in rows[K:] must be allocated and zeroed before calling Encode.
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
	if err := c.enc.Encode(rows); err != nil {
		return nil, fmt.Errorf("failed to encode: %w", err)
	}
	return c.commit(rows, treeBuffer), nil
}

func (c *Coder) validateRows(rows [][]byte) error {
	if len(rows) != c.config.K+c.config.N {
		return fmt.Errorf("expected %d rows, got %d", c.config.K+c.config.N, len(rows))
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
		rlcTree:    rlcTree,
	}
}

// buildRowTree creates the Merkle tree over the K+N extended rows, which are
// already materialized.
func buildRowTree(extended [][]byte, config *Config, buf []byte) *merkle.Tree {
	nodes := buf[:merkle.TreeBufferSize(config.K+config.N)]
	return merkle.NewTreeInto(nodes, extended, config.WorkerCount)
}

// buildRLCTree creates the Merkle tree over the K original RLC values, each
// serialized into the tree-recycled scratch buffer.
func buildRLCTree(rlc rlc.Vector, config *Config, buf []byte) *merkle.Tree {
	nodes := buf[:merkle.TreeBufferSize(config.K)]
	return merkle.NewTreeFuncInto(nodes, config.WorkerCount, func(i int, dst []byte) []byte {
		if cap(dst) == 0 {
			dst = make([]byte, field.GF128Size)
		}
		field.EncodeGF128(dst, rlc[i])
		return dst
	})
}
