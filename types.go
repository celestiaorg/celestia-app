package rsema1d

import "github.com/celestiaorg/rsema1d/field"

// chunkSize is the fixed Leopard chunk size in bytes
const chunkSize = 64

// Commitment is the cryptographic commitment to encoded data
type Commitment = [32]byte // SHA256(rowRoot || rlcRoot)

// ExtendedData holds the encoded data matrix
type ExtendedData struct {
	config     *Config
	rows       [][]byte      // K+N rows of data
	rowRoot    [32]byte      // Merkle root of row hashes
	rlcRoot    [32]byte      // Merkle root of RLC results
	rowHashes  [][]byte      // Cached row hashes
	rlcResults []field.GF128 // Cached RLC results (original rows)
	rowTree    *merkleTree   // Cached row Merkle tree
	rlcTree    *merkleTree   // Cached RLC Merkle tree
}

// Proof represents a proof for a single row
type Proof struct {
	Index    int      // Row index
	Row      []byte   // Row data
	RowProof [][]byte // Merkle proof for row

	// For extended rows (index >= K)
	YOrig      []field.GF128 // Original RLC results
	YLeftProof [][]byte      // Proof from left-subtree

	// For original rows (index < K)
	RLCProof [][]byte // Merkle proof for RLC
}

// ProofType indicates whether proof is for original or extended row
type ProofType int

const (
	ProofTypeOriginal ProofType = iota
	ProofTypeExtended
)

// Type returns the type of proof based on the index and config
func (p *Proof) Type(config *Config) ProofType {
	if p.Index < config.K {
		return ProofTypeOriginal
	}
	return ProofTypeExtended
}