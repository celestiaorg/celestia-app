package rsema1d

import (
	"crypto/sha256"
	"math/rand"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
)

// IncorrectEncoding holds a structurally valid but semantically invalid encoding.
// Any rows (original or parity) may be modified, then the commitment is recomputed
// from the tampered row tree. RLC values are computed from the non-tampered original
// rows only, so tampered rows are detectable via the RLC commutation check while
// all rows have valid Merkle inclusion proofs.
type IncorrectEncoding struct {
	ExtendedData    *ExtendedData
	Commitment      Commitment
	RLCOrig         []field.GF128
	ModifiedIndices []int
	OriginalRows    map[int][]byte
}

// GenerateIncorrectEncoding creates an encoding where selected rows have been
// modified. Any row index in [0, K+N) is valid — both original and parity rows
// can be tampered.
//
// The algorithm:
//  1. Create a valid encoding from the input data.
//  2. Tamper selected rows (XOR random bytes).
//  3. Rebuild the row Merkle tree from the tampered rows.
//  4. Derive new RLC coefficients from the new row root.
//  5. Compute RLC values using the non-tampered original rows (pre-tamper data
//     for any tampered original rows). This ensures non-tampered rows pass the
//     RLC check while tampered rows fail it.
//  6. Build the RLC tree and commitment.
func GenerateIncorrectEncoding(data [][]byte, config *Config, rowIndices []int, rng *rand.Rand) (*IncorrectEncoding, error) {
	// 1. Create a valid encoding
	extData, _, _, err := Encode(data, config)
	if err != nil {
		return nil, err
	}

	// 2. Tamper selected rows, saving pre-tamper data
	originalRows := make(map[int][]byte, len(rowIndices))
	for _, idx := range rowIndices {
		orig := make([]byte, len(extData.rows[idx]))
		copy(orig, extData.rows[idx])
		originalRows[idx] = orig

		numMods := rng.Intn(len(extData.rows[idx])/4) + 1
		for range numMods {
			pos := rng.Intn(len(extData.rows[idx]))
			extData.rows[idx][pos] ^= byte(rng.Intn(255) + 1)
		}
	}

	// 3. Rebuild row Merkle tree from tampered rows
	newRowTree := buildPaddedRowTree(extData.rows, config)
	newRowRoot := newRowTree.Root()

	// 4. Derive new coefficients from the new row root
	newCoeffs := deriveCoefficients(newRowRoot, config)

	// 5. Compute RLC using non-tampered original rows: for any tampered original
	// row, use the saved pre-tamper data so the RLC values stay correct for them.
	rlcRows := make([][]byte, config.K)
	for i := range config.K {
		if orig, tampered := originalRows[i]; tampered {
			rlcRows[i] = orig
		} else {
			rlcRows[i] = extData.rows[i]
		}
	}
	newRLCOrig := computeRLCOrig(rlcRows, newCoeffs, config)

	// 6. Build new RLC Merkle tree
	newRLCOrigTree := BuildPaddedRLCTree(newRLCOrig, config)
	newRLCOrigRoot := newRLCOrigTree.Root()

	// 7. Compute new commitment: SHA256(newRowRoot || newRLCOrigRoot)
	h := sha256.New()
	h.Write(newRowRoot[:])
	h.Write(newRLCOrigRoot[:])
	var commitment Commitment
	h.Sum(commitment[:0])

	// 8. Update ExtendedData
	extData.rowRoot = newRowRoot
	extData.rowTree = newRowTree
	extData.rlcOrig = newRLCOrig
	extData.rlcOrigTree = newRLCOrigTree
	extData.rlcOrigRoot = newRLCOrigRoot

	return &IncorrectEncoding{
		ExtendedData:    extData,
		Commitment:      commitment,
		RLCOrig:         newRLCOrig,
		ModifiedIndices: rowIndices,
		OriginalRows:    originalRows,
	}, nil
}
