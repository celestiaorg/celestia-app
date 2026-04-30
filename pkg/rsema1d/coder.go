package rsema1d

import (
	"crypto/sha256"
	"fmt"

	"github.com/klauspost/reedsolomon"
)

// Coder provides encoding and reconstruction operations with a cached Reed-Solomon encoder.
// Use NewCoder to create an instance and reuse it for multiple operations with the same Config.
type Coder struct {
	config *Config
	enc    reedsolomon.Encoder
}

// NewCoder creates a Coder with cached Reed-Solomon encoder.
func NewCoder(cfg *Config) (*Coder, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	enc, err := reedsolomon.New(cfg.K, cfg.N, reedsolomon.WithLeopardGF16(true))
	if err != nil {
		return nil, fmt.Errorf("failed to create encoder: %w", err)
	}

	return &Coder{config: cfg, enc: enc}, nil
}

// Encode creates parity and commitment for K+N rows.
// rows must have length K+N. Original data goes in rows[:K], and parity rows
// in rows[K:] must be allocated and zeroed before calling Encode.
func (c *Coder) Encode(rows [][]byte) (*ExtendedData, error) {
	if err := c.validateRows(rows); err != nil {
		return nil, err
	}
	if err := c.enc.Encode(rows); err != nil {
		return nil, fmt.Errorf("failed to encode: %w", err)
	}
	return c.commit(rows), nil
}

// Reconstruct fills any missing rows and then computes commitment material for
// the full extended data set. rows must have length K+N. Missing rows may be nil.
func (c *Coder) Reconstruct(rows [][]byte) (*ExtendedData, error) {
	if err := c.validateRows(rows); err != nil {
		return nil, err
	}
	if err := c.enc.Reconstruct(rows); err != nil {
		return nil, fmt.Errorf("failed to reconstruct: %w", err)
	}
	return c.commit(rows), nil
}

func (c *Coder) validateRows(rows [][]byte) error {
	if len(rows) != c.config.K+c.config.N {
		return fmt.Errorf("expected %d rows, got %d", c.config.K+c.config.N, len(rows))
	}
	return nil
}

func (c *Coder) commit(extendedRows [][]byte) *ExtendedData {
	// build padded Merkle tree for rows
	rowTree := buildPaddedRowTree(extendedRows, c.config)
	rowRoot := rowTree.Root()

	// derive RLC coefficients and compute RLC results for original rows.
	// rowSize is taken from the data to support the Coder's deferred-RowSize
	// mode (config.RowSize may be 0 when the Coder is reused across shards).
	coeffs := deriveCoefficients(rowRoot, c.config.K, c.config.N, len(extendedRows[0]))
	rlcOrig := computeRLCVectorized(extendedRows[:c.config.K], coeffs, c.config)

	// build padded RLC Merkle tree
	rlcOrigTree := BuildPaddedRLCTree(rlcOrig, c.config)
	rlcOrigRoot := rlcOrigTree.Root()

	// create commitment: SHA256(rowRoot || rlcOrigRoot)
	h := sha256.New()
	h.Write(rowRoot[:])
	h.Write(rlcOrigRoot[:])
	var commitment Commitment
	h.Sum(commitment[:0])

	return &ExtendedData{
		config:      c.config,
		rows:        extendedRows,
		rowRoot:     rowRoot,
		rlcOrig:     rlcOrig,
		rowTree:     rowTree,
		rlcOrigTree: rlcOrigTree,
		rlcOrigRoot: rlcOrigRoot,
		commitment:  commitment,
	}
}
