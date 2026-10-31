package rsema1d

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d/merkle"
	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d/rlc"
)

// Commitment is the cryptographic commitment to encoded data.
type Commitment = [32]byte // SHA256(rowRoot || rlcOrigRoot)

// ExtendedData holds the K+N row matrix produced by [Coder.Encode] together
// with the merkle structures needed to issue row proofs.
type ExtendedData struct {
	config *Config

	rows     [][]byte // K+N rows of data
	rowsTree *merkle.Tree

	rlc rlc.Vector // RLC results for the K original rows

	commitment Commitment // SHA256(rowRoot || rlcOrigRoot)
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

// RowProofs yields the row data and Merkle proof for each index, carving all
// proofs from a single arena — far cheaper than [ExtendedData.GenerateRowProof]
// per index. row and proof alias ExtendedData storage, valid until it is
// released; yield must not retain them.
func (ed *ExtendedData) RowProofs(indices []int, yield func(index int, row []byte, proof [][]byte)) error {
	return ed.rowsTree.Proofs(indices, func(i int, proof [][]byte) {
		index := indices[i]
		yield(index, ed.rows[index], proof)
	})
}

// RowProof binds a row to the commitment via a Merkle path through the row
// tree. Verified against the rowRoot recovered from the proof, then against
// the commitment together with the expected RLC shard.
type RowProof struct {
	Index    int      // actual row index in [0, K+N)
	Row      []byte   // row data
	RowProof [][]byte // Merkle proof linking Row to rowRoot
}

// GenerateRowProof returns a Merkle proof binding the row at `index` to the
// commitment's rowRoot. Index covers both original (0..K-1) and parity
// (K..K+N-1) rows.
func (ed *ExtendedData) GenerateRowProof(index int) (*RowProof, error) {
	if index < 0 || index >= ed.config.K+ed.config.N {
		return nil, fmt.Errorf("index %d out of range [0, %d)", index, ed.config.K+ed.config.N)
	}
	rowProof, err := ed.rowsTree.Proof(index)
	if err != nil {
		return nil, fmt.Errorf("failed to generate row proof: %w", err)
	}
	return &RowProof{
		Index:    index,
		Row:      ed.rows[index],
		RowProof: rowProof,
	}, nil
}
