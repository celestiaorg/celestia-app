package rsema1d

import (
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/celestiaorg/rsema1d/encoding"
	"github.com/celestiaorg/rsema1d/field"
)

// Encode extends data vertically and creates commitment.
// Returns the extended data structure, the commitment hash, the RLC coefficients
// for original rows, and an error if encoding fails.
func Encode(data [][]byte, config *Config) (*ExtendedData, Commitment, []field.GF128, error) {
	// 1. Validate input
	if err := config.Validate(); err != nil {
		return nil, Commitment{}, nil, fmt.Errorf("invalid config: %w", err)
	}

	if len(data) != config.K {
		return nil, Commitment{}, nil, fmt.Errorf("expected %d rows, got %d", config.K, len(data))
	}

	for i, row := range data {
		if len(row) != config.RowSize {
			return nil, Commitment{}, nil, fmt.Errorf("row %d has size %d, expected %d", i, len(row), config.RowSize)
		}
	}

	// 2. Extend data using Leopard RS
	extended, err := encoding.ExtendVertical(data, config.N)
	if err != nil {
		return nil, Commitment{}, nil, fmt.Errorf("failed to extend data: %w", err)
	}

	// 3. Build padded Merkle tree for rows
	rowTree := buildPaddedRowTree(extended, config)
	rowRoot := rowTree.Root()

	// 4. Derive RLC coefficients
	coeffs := deriveCoefficients(rowRoot, config)

	// 5. Compute RLC results for original rows
	rlcOrig := computeRLCOrig(data, coeffs, config)

	// 6. Extend RLC results
	rlcExtended, err := encoding.ExtendRLCResults(rlcOrig, config.N)
	if err != nil {
		return nil, Commitment{}, nil, fmt.Errorf("failed to extend RLC results: %w", err)
	}

	// 7. Build padded RLC Merkle tree matching row tree structure
	rlcTree := buildPaddedRLCTree(rlcExtended, config)
	rlcRoot := rlcTree.Root()

	// 8. Create commitment: SHA256(rowRoot || rlcRoot)
	h := sha256.New()
	h.Write(rowRoot[:])
	h.Write(rlcRoot[:])
	var commitment Commitment
	h.Sum(commitment[:0])

	// Create ExtendedData
	extData := &ExtendedData{
		config:  config,
		rows:    extended,
		rowRoot: rowRoot,
		rlcRoot: rlcRoot,
		rlcOrig: rlcOrig,
		rowTree: rowTree,
		rlcTree: rlcTree,
	}

	return extData, commitment, rlcOrig, nil
}

// computeRLCOrig computes random linear combinations for original rows
func computeRLCOrig(rows [][]byte, coeffs []field.GF128, config *Config) []field.GF128 {
	results := make([]field.GF128, len(rows))

	var wg sync.WaitGroup
	sem := make(chan struct{}, config.WorkerCount)

	for i, row := range rows {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, r []byte) {
			defer wg.Done()
			defer func() { <-sem }()
			results[idx] = computeRLC(r, coeffs, config)
		}(i, row)
	}
	wg.Wait()

	return results
}

// GenerateRowProof creates a lightweight proof (for use with context)
func (ed *ExtendedData) GenerateRowProof(index int) (*RowProof, error) {
	if index < 0 || index >= ed.config.K+ed.config.N {
		return nil, fmt.Errorf("index %d out of range [0, %d)", index, ed.config.K+ed.config.N)
	}

	// Map actual index to padded tree position
	treeIndex := mapIndexToTreePosition(index, ed.config)

	rowProof, err := ed.rowTree.GenerateProof(treeIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to generate row proof: %w", err)
	}

	return &RowProof{
		Index:    index, // Store actual index, not tree position
		Row:      ed.rows[index],
		RowProof: rowProof,
	}, nil
}

// GenerateStandaloneProof creates a self-contained proof for single row verification
// Best for reading individual original rows without context
func (ed *ExtendedData) GenerateStandaloneProof(index int) (*StandaloneProof, error) {
	if index >= ed.config.K {
		return nil, fmt.Errorf("standalone proofs only supported for original rows (index < K)")
	}

	rowProof, err := ed.GenerateRowProof(index)
	if err != nil {
		return nil, err
	}

	rlcProof, err := ed.rlcTree.GenerateProof(index)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RLC proof: %w", err)
	}

	return &StandaloneProof{
		RowProof: *rowProof,
		RLCProof: rlcProof,
	}, nil
}

// Reconstruct recovers original data from any K rows
func Reconstruct(rows [][]byte, indices []int, config *Config) ([][]byte, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Use the encoding package's Reconstruct function
	return encoding.Reconstruct(rows, indices, config.K, config.N)
}
