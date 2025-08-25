package rsema1d

import (
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/celestiaorg/rsema1d/encoding"
	"github.com/celestiaorg/rsema1d/field"
	"github.com/celestiaorg/rsema1d/merkle"
)

// Encode extends data vertically and creates commitment
func Encode(data [][]byte, config *Config) (*ExtendedData, Commitment, error) {
	// 1. Validate input
	if err := config.Validate(); err != nil {
		return nil, Commitment{}, fmt.Errorf("invalid config: %w", err)
	}

	if len(data) != config.K {
		return nil, Commitment{}, fmt.Errorf("expected %d rows, got %d", config.K, len(data))
	}

	for i, row := range data {
		if len(row) != config.RowSize {
			return nil, Commitment{}, fmt.Errorf("row %d has size %d, expected %d", i, len(row), config.RowSize)
		}
	}

	// 2. Extend data using Leopard RS
	extended, err := encoding.ExtendVertical(data, config.N)
	if err != nil {
		return nil, Commitment{}, fmt.Errorf("failed to extend data: %w", err)
	}

	// 3. Compute row hashes and Merkle tree
	rowHashes := computeRowHashes(extended, config.WorkerCount)
	rowTree := merkle.NewTree(rowHashes)
	rowRoot := rowTree.Root()

	// 4. Derive RLC coefficients
	coeffs := deriveCoefficients(rowRoot, config)

	// 5. Compute RLC results for original rows
	rlcOrig := computeRLCOrig(data, coeffs, config)

	// 6. Extend RLC results
	rlcExtended, err := encoding.ExtendRLCResults(rlcOrig, config.N)
	if err != nil {
		return nil, Commitment{}, fmt.Errorf("failed to extend RLC results: %w", err)
	}

	// 7. Build RLC Merkle tree
	rlcLeaves := make([][]byte, len(rlcExtended))
	for i, result := range rlcExtended {
		bytes := field.ToBytes128(result)
		rlcLeaves[i] = bytes[:]
	}
	rlcTree := merkle.NewTree(rlcLeaves)
	rlcRoot := rlcTree.Root()

	// 8. Create commitment: SHA256(rowRoot || rlcRoot)
	h := sha256.New()
	h.Write(rowRoot[:])
	h.Write(rlcRoot[:])
	var commitment Commitment
	h.Sum(commitment[:0])

	// Create ExtendedData
	extData := &ExtendedData{
		config:     config,
		rows:       extended,
		rowRoot:    rowRoot,
		rlcRoot:    rlcRoot,
		rowHashes:  rowHashes,
		rlcOrig:    rlcOrig,
		rowTree:    rowTree,
		rlcTree:    rlcTree,
	}

	return extData, commitment, nil
}

// computeRowHashes computes SHA256 hashes of all rows
func computeRowHashes(rows [][]byte, workerCount int) [][]byte {
	hashes := make([][]byte, len(rows))

	var wg sync.WaitGroup
	sem := make(chan struct{}, workerCount)

	for i, row := range rows {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, r []byte) {
			defer wg.Done()
			defer func() { <-sem }()
			hash := sha256.Sum256(r)
			hashes[idx] = hash[:]
		}(i, row)
	}
	wg.Wait()

	return hashes
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

// GenerateProof creates a proof for the specified row
func (ed *ExtendedData) GenerateProof(index int) (*Proof, error) {
	if index < 0 || index >= ed.config.K+ed.config.N {
		return nil, fmt.Errorf("index %d out of range [0, %d)", index, ed.config.K+ed.config.N)
	}

	// Get row data
	row := ed.rows[index]

	// Generate row Merkle proof
	rowProof, err := ed.rowTree.GenerateProof(index)
	if err != nil {
		return nil, fmt.Errorf("failed to generate row proof: %w", err)
	}

	proof := &Proof{
		Index:    index,
		Row:      row,
		RowProof: rowProof,
	}

	// Branch based on row type
	if index < ed.config.K {
		// Original row - add RLC proof
		rlcProof, err := ed.rlcTree.GenerateProof(index)
		if err != nil {
			return nil, fmt.Errorf("failed to generate RLC proof: %w", err)
		}
		proof.RLCProof = rlcProof
	} else {
		// Extended row - add all original RLC results (no subtree proof needed)
		// Serialize RLC results for wire format
		proof.RLCOrig = make([][]byte, len(ed.rlcOrig))
		for i, rlc := range ed.rlcOrig {
			bytes := field.ToBytes128(rlc)
			proof.RLCOrig[i] = bytes[:]
		}
	}

	return proof, nil
}


// Reconstruct recovers original data from any K rows
func Reconstruct(rows [][]byte, indices []int, config *Config) ([][]byte, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	
	// Use the encoding package's Reconstruct function
	return encoding.Reconstruct(rows, indices, config.K, config.N)
}

