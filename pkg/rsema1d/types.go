package rsema1d

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/merkle"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/rlc"
)

// Commitment is the cryptographic commitment to encoded data.
type Commitment = [32]byte // SHA256(rowRoot || rlcOrigRoot)

// ExtendedData holds the K+N row matrix produced by [Coder.Encode] together
// with the merkle structures needed to issue row proofs.
type ExtendedData struct {
	config      *Config
	rows        [][]byte     // K+N rows of data
	rowRoot     merkle.Root  // Merkle root of rows
	rlcOrig     rlc.Vector   // RLC results for the K original rows
	rowTree     *merkle.Tree // Cached row Merkle tree
	rlcOrigTree *merkle.Tree // Cached RLC Merkle tree
	rlcOrigRoot merkle.Root  // Cached RLC root
	commitment  Commitment   // SHA256(rowRoot || rlcOrigRoot)
}

// Commitment returns the cryptographic commitment for this extended data.
func (ed *ExtendedData) Commitment() Commitment {
	return ed.commitment
}

// RLC returns the random linear combination values for the K original rows.
func (ed *ExtendedData) RLC() rlc.Vector {
	return ed.rlcOrig
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
// (K..K+N-1) rows; the helper handles the padded-tree position mapping so
// callers can use the natural data-space index.
func (ed *ExtendedData) GenerateRowProof(index int) (*RowProof, error) {
	if index < 0 || index >= ed.config.K+ed.config.N {
		return nil, fmt.Errorf("index %d out of range [0, %d)", index, ed.config.K+ed.config.N)
	}
	treeIndex := mapIndexToTreePosition(index, ed.config)
	rowProof, err := ed.rowTree.GenerateProof(treeIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to generate row proof: %w", err)
	}
	return &RowProof{
		Index:    index, // actual data index, not the tree position
		Row:      ed.rows[index],
		RowProof: rowProof,
	}, nil
}

// StandaloneProof is the self-contained proof for an original row defined in
// SPEC §3.4 / §3.5 case 3: a reader that doesn't have rlcOrig can verify a
// single original row by checking it against the commitment using the
// embedded row proof plus an RLC proof linking the row's RLC value to the
// rlcOrigRoot.
type StandaloneProof struct {
	RowProof
	RLCProof [][]byte // Merkle proof linking RLC(Row) to rlcOrigRoot
}

// GenerateStandaloneProof builds the row + RLC merkle proofs needed to verify
// an original row in isolation. Only original rows (index < K) are supported
// since parity rows are recovered from the K originals via Reed-Solomon and
// don't have a slot in the rlcOrigRoot tree.
func (ed *ExtendedData) GenerateStandaloneProof(index int) (*StandaloneProof, error) {
	if index >= ed.config.K {
		return nil, fmt.Errorf("standalone proofs only supported for original rows (index < K = %d)", ed.config.K)
	}
	rowProof, err := ed.GenerateRowProof(index)
	if err != nil {
		return nil, err
	}
	rlcProof, err := ed.rlcOrigTree.GenerateProof(index)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RLC proof: %w", err)
	}
	return &StandaloneProof{
		RowProof: *rowProof,
		RLCProof: rlcProof,
	}, nil
}
