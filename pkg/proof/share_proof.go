package proof

import (
	"errors"
	"fmt"
	"math"

	"github.com/celestiaorg/nmt"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
)

// Validate runs basic validations on the proof then verifies if it is consistent.
// It returns nil if the proof is valid. Otherwise, it returns a sensible error.
// The `root` is the block data root that the shares to be proven belong to.
// Note: these proofs are tested on the app side.
func (sp ShareProof) Validate(root []byte) error {
	if sp.Data == nil {
		return errors.New("empty share proof")
	}

	numberOfSharesInProofs := int32(0)
	for _, proof := range sp.ShareProofs {
		// the range is not inclusive from the left.
		numberOfSharesInProofs += proof.End - proof.Start
	}

	if len(sp.ShareProofs) != len(sp.RowProof.RowRoots) {
		return fmt.Errorf("the number of share proofs %d must equal the number of row roots %d", len(sp.ShareProofs), len(sp.RowProof.RowRoots))
	}
	if len(sp.Data) != int(numberOfSharesInProofs) {
		return fmt.Errorf("the number of shares %d must equal the number of shares in share proofs %d", len(sp.Data), numberOfSharesInProofs)
	}

	for _, proof := range sp.ShareProofs {
		if proof.Start < 0 {
			return errors.New("proof index cannot be negative")
		}
		if (proof.End - proof.Start) <= 0 {
			return errors.New("proof total must be positive")
		}
	}

	if err := sp.RowProof.Validate(root); err != nil {
		return err
	}

	if ok := sp.VerifyProof(); !ok {
		return errors.New("share proof failed to verify")
	}

	return nil
}

func (sp ShareProof) VerifyProof() bool {
	cursor := int32(0)
	for i, proof := range sp.ShareProofs {
		nmtProof := nmt.NewInclusionProof(
			int(proof.Start),
			int(proof.End),
			proof.Nodes,
			true,
		)
		sharesUsed := proof.End - proof.Start
		if sp.NamespaceVersion > math.MaxUint8 {
			return false
		}
		// Consider extracting celestia-app's namespace package. We can't use it
		// here because that would introduce a circular import.
		namespace := append([]byte{uint8(sp.NamespaceVersion)}, sp.NamespaceId...)
		valid := nmtProof.VerifyInclusion(
			appconsts.NewBaseHashFunc(),
			namespace,
			sp.Data[cursor:sharesUsed+cursor],
			sp.RowProof.RowRoots[i],
		)
		if !valid {
			return false
		}
		cursor += sharesUsed
	}
	return true
}
