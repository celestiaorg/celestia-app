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
	// HACKHACK performing subtraction with unsigned integers is unsafe.
	if int(rp.EndRow-rp.StartRow+1) != len(rp.RowRoots) {
		return fmt.Errorf("the number of rows %d must equal the number of row roots %d", int(rp.EndRow-rp.StartRow+1), len(rp.RowRoots))
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

func (p *Proof) Verify(rootHash []byte, leaf []byte) error {
	proof := &merkle.Proof{
		Total:    p.Total,
		Index:    p.Index,
		LeafHash: p.LeafHash,
		Aunts:    p.Aunts,
	}
	return proof.Verify(rootHash, leaf)
}
