package rsema1d

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/rlc"
)

// Commitment is the cryptographic commitment to encoded data.
type Commitment = [32]byte // BLAKE3(rowRoot || rlcOrigRoot)

// ExtendedData holds the K+N row matrix produced by [Coder.Encode] together
// with the structures needed to issue row proofs. The row dimension is
// committed with a BLAKE3-Bao tree; the RLC dimension stays an explicit
// binary Merkle tree.
type ExtendedData struct {
	config *Config

	rows    [][]byte    // K+N rows of data
	baoRow  *baoRowTree // BLAKE3-Bao tree over the extended rows
	rowRoot [32]byte    // BLAKE3-Bao root of the extended rows

	rlc rlc.Vector // RLC results for the K original rows

	commitment Commitment // BLAKE3(rowRoot || rlcOrigRoot)
}

// Commitment returns the cryptographic commitment for this extended data.
func (ed *ExtendedData) Commitment() Commitment {
	return ed.commitment
}

// RLC returns the random linear combination values for the K original rows.
func (ed *ExtendedData) RLC() rlc.Vector {
	return ed.rlc
}

// Row returns the row at the given index in [0, K+N). Originals occupy
// [0, K); parity rows occupy [K, K+N). The returned slice aliases the
// internal storage — callers must not mutate it.
func (ed *ExtendedData) Row(index int) []byte {
	return ed.rows[index]
}

// RowProofs yields the row data and Bao slice for each index. row and slice
// alias ExtendedData storage / freshly carved slices, valid until it is
// released; yield must not retain them. root is the shared BLAKE3-Bao row root.
func (ed *ExtendedData) RowProofs(indices []int, yield func(index int, row []byte, slice []byte, root [32]byte)) error {
	for _, index := range indices {
		if index < 0 || index >= ed.config.K+ed.config.N {
			return fmt.Errorf("index %d out of range [0, %d)", index, ed.config.K+ed.config.N)
		}
		slice, err := ed.baoRow.generateRowSlice(index, ed.rows[index])
		if err != nil {
			return fmt.Errorf("failed to generate row slice: %w", err)
		}
		yield(index, ed.rows[index], slice, ed.rowRoot)
	}
	return nil
}

// RowProof binds a row to the commitment via a BLAKE3-Bao slice through the row
// tree. Verified by recomputing the leaf chaining value from Row and walking
// the sibling path in Slice up to RowRoot, then checking RowRoot against the
// commitment together with the expected RLC shard.
type RowProof struct {
	Index   int      // actual row index in [0, K+N)
	Row     []byte   // row data (unpadded)
	Slice   []byte   // Bao sibling path opening Row against RowRoot
	RowRoot [32]byte // BLAKE3-Bao root of the extended row data
}

// GenerateRowProof returns a Bao slice binding the row at `index` to the
// commitment's rowRoot. Index covers both original (0..K-1) and parity
// (K..K+N-1) rows.
func (ed *ExtendedData) GenerateRowProof(index int) (*RowProof, error) {
	if index < 0 || index >= ed.config.K+ed.config.N {
		return nil, fmt.Errorf("index %d out of range [0, %d)", index, ed.config.K+ed.config.N)
	}
	slice, err := ed.baoRow.generateRowSlice(index, ed.rows[index])
	if err != nil {
		return nil, fmt.Errorf("failed to generate row proof: %w", err)
	}
	return &RowProof{
		Index:   index,
		Row:     ed.rows[index],
		Slice:   slice,
		RowRoot: ed.rowRoot,
	}, nil
}
