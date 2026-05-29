package rsema1d

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math/bits"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/merkle"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/rlc"
)

// VerifyStandaloneProof checks a StandaloneProof against a commitment per
// SPEC §3.5 case 3: rebuild the row root from the row proof, derive
// coefficients, compute the row's RLC, rebuild the rlcOrigRoot from the RLC
// proof, and verify SHA256(rowRoot || rlcOrigRoot) == commitment.
//
// Only original-row proofs (Index < K) are accepted; parity rows must be
// verified through the batched Verifier path against a known rlcOrig.
func VerifyStandaloneProof(proof *StandaloneProof, commitment Commitment, config *Config) error {
	if proof == nil {
		return errors.New("nil standalone proof")
	}
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	if proof.Index < 0 || proof.Index >= config.K {
		return fmt.Errorf("standalone verification only supports original rows (got index %d, K=%d)", proof.Index, config.K)
	}

	rowTreeDepth := bits.Len(uint(config.totalPadded)) - 1
	if len(proof.RowProof.RowProof) != rowTreeDepth {
		return fmt.Errorf("row proof depth mismatch: expected %d, got %d", rowTreeDepth, len(proof.RowProof.RowProof))
	}
	rlcTreeDepth := bits.Len(uint(config.kPadded)) - 1
	if len(proof.RLCProof) != rlcTreeDepth {
		return fmt.Errorf("rlc proof depth mismatch: expected %d, got %d", rlcTreeDepth, len(proof.RLCProof))
	}

	// 1. Recover rowRoot from the row's Merkle proof.
	rowRoot, err := merkle.ComputeRootFromProof(proof.Row, proof.Index, proof.RowProof.RowProof)
	if err != nil {
		return fmt.Errorf("computing row root: %w", err)
	}

	// 2. Derive coefficients and recompute the row's RLC scalar-wise.
	coeffs := rlc.Derive(rowRoot, config.K, config.N, len(proof.Row), config.WorkerCount)
	rlcValue := rlc.ComputeRow(proof.Row, coeffs)

	// 3. Recover rlcOrigRoot from the RLC's Merkle proof.
	var rlcBytes [field.GF128Size]byte
	field.EncodeGF128(rlcBytes[:], rlcValue)
	rlcOrigRoot, err := merkle.ComputeRootFromProof(rlcBytes[:], proof.Index, proof.RLCProof)
	if err != nil {
		return fmt.Errorf("computing RLC root: %w", err)
	}

	// 4. Verify the commitment matches SHA256(rowRoot || rlcOrigRoot).
	h := sha256.New()
	h.Write(rowRoot[:])
	h.Write(rlcOrigRoot[:])
	var computed Commitment
	h.Sum(computed[:0])
	if computed != commitment {
		return errors.New("commitment verification failed")
	}
	return nil
}
