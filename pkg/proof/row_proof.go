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
	if !rp.VerifyProof(root) {
		return errors.New("row proof failed to verify")
	}

	return nil
}

// VerifyProof verifies that all the row roots in this RowProof exist in a
// Merkle tree with the given root. Returns true if all proofs are valid.
func (rp RowProof) VerifyProof(root []byte) bool {
	for i, proof := range rp.Proofs {
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
