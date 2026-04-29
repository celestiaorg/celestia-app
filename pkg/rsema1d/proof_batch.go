package rsema1d

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math/bits"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/merkle"
)

// VerifyRowsWithContext verifies N row proofs against the same commitment in a
// single batched RLC computation. Replaces N calls to computeRLC with one
// computeRLCVectorized call, amortizing the SIMD GF(2^16) kernel setup across
// all rows.
//
// Per-row Merkle proof verification and RLC/commitment comparisons still happen
// individually. Returns the first error encountered, naming the offending row.
func VerifyRowsWithContext(proofs []*RowProof, commitment Commitment, context *VerificationContext) error {
	if context == nil {
		return errors.New("received nil context in verifier")
	}
	if len(proofs) == 0 {
		return nil
	}

	// Validate every proof's shape before any heavy compute. The nil-check
	// has to run before the proofs[0] dereference inside the loop.
	kPadded := nextPowerOfTwo(context.config.K)
	totalPadded := nextPowerOfTwo(kPadded + context.config.N)
	expectedProofDepth := bits.Len(uint(totalPadded)) - 1
	rowSize := context.config.RowSize
	for i, p := range proofs {
		if p == nil {
			return errors.New("received nil proof in verifier")
		}
		if i == 0 && rowSize == 0 {
			rowSize = len(p.Row)
		}
		if p.Index < 0 || p.Index >= context.config.K+context.config.N {
			return fmt.Errorf("index %d out of range [0, %d)", p.Index, context.config.K+context.config.N)
		}
		if context.config.RowSize > 0 && len(p.Row) != context.config.RowSize {
			return fmt.Errorf("row %d: row size mismatch: expected %d, got %d", p.Index, context.config.RowSize, len(p.Row))
		}
		if len(p.Row) != rowSize {
			return fmt.Errorf("batched verify requires equal-sized rows: row %d has %d bytes, expected %d",
				p.Index, len(p.Row), rowSize)
		}
		if len(p.RowProof) != expectedProofDepth {
			return fmt.Errorf("row %d: proof depth mismatch: expected %d, got %d", p.Index, expectedProofDepth, len(p.RowProof))
		}
		if p.Index >= len(context.rlcExtended) {
			return fmt.Errorf("index %d out of range", p.Index)
		}
	}

	// Guard against rowSize values that would cause computeRLCVectorized to
	// divide by zero (numChunks == 0). Config.Validate covers config.RowSize,
	// but variable-size mode (RowSize == 0) derives rowSize from proofs[0].
	if rowSize == 0 || rowSize%chunkSize != 0 {
		return fmt.Errorf("row size must be a positive multiple of %d, got %d", chunkSize, rowSize)
	}

	// Verify each row's Merkle proof independently so tampering with any
	// single row fails, even though all rowRoots are identical in a valid shard.
	rowRoots := make([][32]byte, len(proofs))
	for i, p := range proofs {
		treeIndex := mapIndexToTreePosition(p.Index, context.config)
		rowRoot, err := merkle.ComputeRootFromProof(p.Row, treeIndex, p.RowProof)
		if err != nil {
			return fmt.Errorf("failed to compute row root for row %d: %w", p.Index, err)
		}
		rowRoots[i] = rowRoot
	}

	// Choice of rowRoots[0] is arbitrary — all are equal in a valid shard,
	// and the per-row commitment check below rejects the batch if any differ.
	coeffs := deriveCoefficients(rowRoots[0], len(proofs[0].Row))

	rows := make([][]byte, len(proofs))
	for i, p := range proofs {
		rows[i] = p.Row
	}
	computedRLCs := computeRLCVectorized(rows, coeffs, context.config)

	h := sha256.New()
	for i, p := range proofs {
		if !field.Equal128(computedRLCs[i], context.rlcExtended[p.Index]) {
			return fmt.Errorf("row %d: computed RLC does not match expected value", p.Index)
		}

		h.Reset()
		h.Write(rowRoots[i][:])
		h.Write(context.rlcOrigRoot[:])
		var computedCommitment [32]byte
		h.Sum(computedCommitment[:0])
		if commitment != computedCommitment {
			return fmt.Errorf("row %d: commitment verification failed", p.Index)
		}
	}

	return nil
}
