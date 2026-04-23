package rsema1d

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math/bits"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/merkle"
)

// minBatchedVerifyK is the smallest batch size where computeRLCVectorized is
// faster per-row than the scalar Kernel. Measured on Intel c6id.8xlarge
// (AVX-512 + GFNI). Below this threshold, fall back to scalar VerifyRowWithContext
// which avoids the zero-row padding overhead of the vectorized path.
const minBatchedVerifyK = 8

// VerifyRowsWithContext verifies N row proofs against the same commitment in a
// single batched RLC computation. When len(proofs) ≥ minBatchedVerifyK it
// replaces N calls to computeRLC with one computeRLCVectorized call, amortizing
// the SIMD GF(2^16) kernel setup across all rows.
//
// Per-row Merkle proof verification and RLC/commitment comparisons still happen
// individually. Returns the first error encountered, matching the fail-fast
// semantics of a loop over VerifyRowWithContext.
func VerifyRowsWithContext(proofs []*RowProof, commitment Commitment, context *VerificationContext) error {
	if context == nil {
		return errors.New("received nil context in verifier")
	}
	if len(proofs) == 0 {
		return nil
	}

	// Below the break-even, per-row SIMD would pad each batch up to K=32 and
	// pay more than a scalar computeRLCKernel call. Fall back to the existing
	// scalar loop which is alloc-free.
	if len(proofs) < minBatchedVerifyK {
		for _, p := range proofs {
			if err := VerifyRowWithContext(p, commitment, context); err != nil {
				return err
			}
		}
		return nil
	}

	// 1. Validate every proof's shape up-front (index range, row sizes, proof
	// depths) before doing any heavy compute. This matches the checks inside
	// VerifyRowWithContext but lets us fail fast without ever running the RLC.
	// Nil-checks come before any proofs[0] dereference so a nil first element
	// returns an error instead of panicking.
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
			return fmt.Errorf("row size mismatch: expected %d, got %d", context.config.RowSize, len(p.Row))
		}
		if len(p.Row) != rowSize {
			return fmt.Errorf("batched verify requires equal-sized rows: row %d has %d bytes, expected %d",
				p.Index, len(p.Row), rowSize)
		}
		if len(p.RowProof) != expectedProofDepth {
			return fmt.Errorf("row proof depth mismatch: expected %d, got %d", expectedProofDepth, len(p.RowProof))
		}
		if p.Index >= len(context.rlcExtended) {
			return fmt.Errorf("index %d out of range", p.Index)
		}
	}

	// 2. Verify every Merkle proof and collect the computed rowRoot per row. In
	// a valid shard, every rowRoot is identical (they all come from the same
	// row tree), but we check each independently so tampering with any single
	// row still fails.
	rowRoots := make([][32]byte, len(proofs))
	for i, p := range proofs {
		treeIndex := mapIndexToTreePosition(p.Index, context.config)
		rowRoot, err := merkle.ComputeRootFromProof(p.Row, treeIndex, p.RowProof)
		if err != nil {
			return fmt.Errorf("failed to compute row root for row %d: %w", p.Index, err)
		}
		rowRoots[i] = rowRoot
	}

	// 3. Derive coefficients once, using the same sync.Once cache as the
	// scalar path so subsequent verify calls on this context hit the cache.
	context.coeffsOnce.Do(func() {
		context.coeffs = deriveCoefficients(rowRoots[0], len(proofs[0].Row))
	})
	if len(context.coeffs) != len(proofs[0].Row)/2 {
		return fmt.Errorf("row size mismatch: cached coefficients for %d bytes, got %d",
			len(context.coeffs)*2, len(proofs[0].Row))
	}

	// 4. Batched RLC: one vectorized pass over all rows instead of len(proofs)
	// scalar calls. This is where the per-row cost drops from ~540 µs to ~50
	// µs at K≥32 on AVX-512 + GFNI.
	rows := make([][]byte, len(proofs))
	for i, p := range proofs {
		rows[i] = p.Row
	}
	computedRLCs := computeRLCVectorized(rows, context.coeffs, context.config)

	// 5. Per-row commitment + RLC checks (cheap, keep in a tight loop).
	for i, p := range proofs {
		if !field.Equal128(computedRLCs[i], context.rlcExtended[p.Index]) {
			return fmt.Errorf("row %d: computed RLC does not match expected value", p.Index)
		}

		h := sha256.New()
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
