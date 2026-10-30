package proof

import (
	"errors"
	"fmt"

	"github.com/cometbft/cometbft/crypto/merkle"
)

// Validate performs checks on the fields of this RowProof. Returns an error if
// the proof fails validation. If the proof passes validation, this function
// attempts to verify the proof. It returns nil if the proof is valid.
func (rp RowProof) Validate(root []byte) error {
	if rp.EndRow < rp.StartRow {
		return fmt.Errorf("end row %d must be greater than or equal to start row %d", rp.EndRow, rp.StartRow)
	}
	expectedRows := int64(rp.EndRow) - int64(rp.StartRow) + 1
	if expectedRows != int64(len(rp.RowRoots)) {
		return fmt.Errorf("the number of rows %d must equal the number of row roots %d", expectedRows, len(rp.RowRoots))
	}
	if len(rp.RowRoots) == 0 {
		return errors.New("row proof must contain at least one row root")
	}
	if len(rp.Proofs) != len(rp.RowRoots) {
		return fmt.Errorf("the number of proofs %d must equal the number of row roots %d", len(rp.Proofs), len(rp.RowRoots))
	}
	for i, proof := range rp.Proofs {
		if proof == nil {
			return fmt.Errorf("nil proof for row %d", int64(rp.StartRow)+int64(i))
		}
		if expected := int64(rp.StartRow) + int64(i); proof.Index != expected {
			return fmt.Errorf("proof index %d does not match claimed row %d", proof.Index, expected)
		}
		if proof.Total != rp.Proofs[0].Total {
			return fmt.Errorf("proof total %d does not match the first proof's total %d", proof.Total, rp.Proofs[0].Total)
		}
		// The data root commits to a power-of-two number of roots.
		if proof.Total < 4 || proof.Total&(proof.Total-1) != 0 {
			return fmt.Errorf("proof total %d must be a power of two of at least 4", proof.Total)
		}
		// The data root commits to the row roots followed by the column
		// roots, so a row root must live in the first half of the leaves.
		if proof.Index >= proof.Total/2 {
			return fmt.Errorf("proof index %d is not a row root index (total leaves %d)", proof.Index, proof.Total)
		}
	}
	if !rp.VerifyProof(root) {
		return errors.New("row proof failed to verify")
	}

	return nil
}

// VerifyProof verifies that all the row roots in this RowProof exist in a
// Merkle tree with the given root. Returns true if all proofs are valid.
func (rp RowProof) VerifyProof(root []byte) bool {
	if len(rp.Proofs) != len(rp.RowRoots) {
		return false
	}
	for i, proof := range rp.Proofs {
		if proof == nil {
			return false
		}
		err := proof.Verify(root, rp.RowRoots[i])
		if err != nil {
			return false
		}
	}
	return true
}

func (p *Proof) Verify(rootHash, leaf []byte) error {
	proof := &merkle.Proof{
		Total:    p.Total,
		Index:    p.Index,
		LeafHash: p.LeafHash,
		Aunts:    p.Aunts,
	}
	return proof.Verify(rootHash, leaf)
}
