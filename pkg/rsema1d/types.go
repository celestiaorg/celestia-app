package rsema1d

import (
	"sync"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/merkle"
)

// chunkSize is the fixed Leopard chunk size in bytes
const chunkSize = 64

// Commitment is the cryptographic commitment to encoded data
type Commitment = [32]byte // SHA256(rowRoot || rlcOrigRoot)

// ExtendedData holds the encoded data matrix
type ExtendedData struct {
	config      *Config
	rows        [][]byte      // K+N rows of data
	rowRoot     [32]byte      // Merkle root of rows
	rlcOrig     []field.GF128 // Cached RLC results (original rows)
	rowTree     *merkle.Tree  // Cached row Merkle tree
	rlcOrigTree *merkle.Tree  // Cached RLC Merkle tree
	rlcOrigRoot [32]byte      // Cached RLC root
	commitment  Commitment    // SHA256(rowRoot || rlcOrigRoot)
}

// Commitment returns the cryptographic commitment for this extended data.
func (ed *ExtendedData) Commitment() Commitment {
	return ed.commitment
}

// RLC returns the computed random linear combination values for the original rows.
func (ed *ExtendedData) RLC() []field.GF128 {
	return ed.rlcOrig
}

// VerificationContext holds precomputed RLC data for efficient batch verification
type VerificationContext struct {
	config      *Config
	rlcOrig     []field.GF128 // Original K RLC values
	rlcExtended []field.GF128 // Extended K+N RLC values
	rlcOrigRoot [32]byte      // Cached RLC root

	coeffsOnce sync.Once
	coeffs     []field.GF128

	// coeffLogOnce gates a lazy build of the per-row LogTab precompute used
	// by the scalar verify fast path. The first scalar verify in this
	// context pays ~128 µs to populate coeffLog; subsequent scalar verifies
	// hit the cache and pay only the computeRLCLogTab inner loop.
	coeffLogOnce sync.Once
	coeffLog     *rlcCoeffLog
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

// RowInclusionProof verifies row inclusion in commitment without RLC verification.
// Works for both original and parity rows. Only verifies that the row is part
// of the committed data, not that RLC computation is correct.
type RowInclusionProof struct {
	RowProof
	RLCRoot [32]byte // RLC root for commitment verification
}
