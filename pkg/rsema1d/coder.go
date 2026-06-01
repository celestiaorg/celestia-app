package rsema1d

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/rlc"
	"github.com/klauspost/reedsolomon"
	"lukechampine.com/blake3"
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

// EncodeWithTree is retained for API compatibility with callers that pre-size a
// commitment scratch buffer. treeBuffer is now ignored: the row commitment is a
// BLAKE3-Bao tree that allocates its own nodes, and the RLC commitment is a flat
// hash, so no external node storage is needed.
func (c *Coder) EncodeWithTree(rows [][]byte, _ []byte) (*ExtendedData, error) {
	if err := c.validateRows(rows); err != nil {
		return nil, err
	}
	if err := c.enc.Encode(rows); err != nil {
		return nil, fmt.Errorf("failed to encode: %w", err)
	}
	return c.commit(rows)
}

func (c *Coder) validateRows(rows [][]byte) error {
	if len(rows) != c.config.K+c.config.N {
		return fmt.Errorf("expected %d rows, got %d", c.config.K+c.config.N, len(rows))
	}
	return nil
}

// commit builds the BLAKE3-Bao row tree, derives the RLC, and forms the
// commitment BLAKE3(rowRoot || rlcCommitment). The RLC is committed with a flat
// hash rather than a Merkle tree: the only consumer (the batched Verifier) is
// always handed the full RLC vector, so per-element openings were never needed.
func (c *Coder) commit(extendedRows [][]byte) (*ExtendedData, error) {
	baoRow, err := buildBaoRowTree(extendedRows, len(extendedRows[0]))
	if err != nil {
		return nil, fmt.Errorf("building bao row tree: %w", err)
	}
	rowRoot := baoRow.root

	// derive RLC coefficients and compute RLC results for original rows.
	coeffs := rlc.Derive(rowRoot, c.config.K, c.config.N, len(extendedRows[0]), c.config.WorkerCount)
	rlcVec := rlc.Compute(extendedRows[:c.config.K], coeffs, c.config.WorkerCount)

	// commitment: BLAKE3(rowRoot || BLAKE3(rlc))
	rlcCommit := rlcCommitment(rlcVec)
	h := blake3.New(32, nil)
	h.Write(rowRoot[:])
	h.Write(rlcCommit[:])
	var commitment Commitment
	h.Sum(commitment[:0])

	return &ExtendedData{
		config:     c.config,
		rows:       extendedRows,
		rlc:        rlcVec,
		commitment: commitment,
		baoRow:     baoRow,
		rowRoot:    rowRoot,
	}, nil
}

// rlcCommitment hashes the K original RLC values into a single 32-byte
// commitment over their canonical GF128 serialization. Producer (commit) and
// consumer (Verifier.setRLC) must agree on this exactly.
func rlcCommitment(vec rlc.Vector) [32]byte {
	h := blake3.New(32, nil)
	var buf [field.GF128Size]byte
	for i := range vec {
		field.EncodeGF128(buf[:], vec[i])
		h.Write(buf[:])
	}
	var out [32]byte
	h.Sum(out[:0])
	return out
}
