package rsema1d

import (
	"github.com/celestiaorg/rsema1d/field"
	"github.com/celestiaorg/rsema1d/merkle"
)

// chunkSize is the fixed Leopard chunk size in bytes
const chunkSize = 64

// Commitment is the cryptographic commitment to encoded data
type Commitment = [32]byte // SHA256(rowRoot || rlcRoot)

// ExtendedData holds the encoded data matrix
type ExtendedData struct {
	config  *Config
	rows    [][]byte      // K+N rows of data
	rowRoot [32]byte      // Merkle root of rows
	rlcRoot [32]byte      // Merkle root of RLC results
	rlcOrig []field.GF128 // Cached RLC results (original rows)
	rowTree *merkle.Tree  // Cached row Merkle tree
	rlcTree *merkle.Tree  // Cached RLC Merkle tree
}

// VerificationContext holds precomputed RLC data for efficient batch verification
type VerificationContext struct {
	config  *Config
	rlcOrig []field.GF128 // Original K RLC values
	rlcRoot [32]byte      // Cached RLC root
}

// RowProof is a lightweight proof without RLC data
type RowProof struct {
	Index    int      // Row index
	Row      []byte   // Row data
	RowProof [][]byte // Merkle proof for row
}

// StandaloneProof includes everything needed for single-row verification
type StandaloneProof struct {
	RowProof
	RLCProof [][]byte // Merkle proof for RLC (original rows only)
}
