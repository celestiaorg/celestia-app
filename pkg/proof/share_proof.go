package proof

import (
	"errors"
	"fmt"
	"math"

	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
	"github.com/celestiaorg/nmt"
)

// Validate runs basic validations on the proof then verifies if it is consistent.
// It returns nil if the proof is valid. Otherwise, it returns a sensible error.
// The `root` is the block data root that the shares to be proven belong to.
// Note: these proofs are tested on the app side.
func (sp ShareProof) Validate(root []byte) error {
	if sp.Data == nil {
		return errors.New("empty share proof")
	}

	if len(sp.ShareProofs) != len(sp.RowProof.RowRoots) {
		return fmt.Errorf("the number of share proofs %d must equal the number of row roots %d", len(sp.ShareProofs), len(sp.RowProof.RowRoots))
	}

	// Sum into int64 so the running total does not wrap. Also check each
	// per-proof span before adding so a single malformed entry is caught
	// before it can poison the sum.
	var numberOfSharesInProofs int64
	for _, proof := range sp.ShareProofs {
		if proof.Start < 0 {
			return errors.New("proof index cannot be negative")
		}
		if proof.End <= proof.Start {
			return errors.New("proof total must be positive")
		}
		numberOfSharesInProofs += int64(proof.End) - int64(proof.Start)
		if numberOfSharesInProofs > int64(len(sp.Data)) {
			return fmt.Errorf("the number of shares in share proofs exceeds the number of shares %d", len(sp.Data))
		}
	}
	if int64(len(sp.Data)) != numberOfSharesInProofs {
		return fmt.Errorf("the number of shares %d must equal the number of shares in share proofs %d", len(sp.Data), numberOfSharesInProofs)
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
	if sp.NamespaceVersion > math.MaxUint8 {
		return false
	}
	cursor := int64(0)
	dataLen := int64(len(sp.Data))
	for i, proof := range sp.ShareProofs {
		if proof.Start < 0 || proof.End <= proof.Start {
			return false
		}
		sharesUsed := int64(proof.End) - int64(proof.Start)
		if cursor+sharesUsed > dataLen {
			return false
		}
		nmtProof := nmt.NewInclusionProof(
			int(proof.Start),
			int(proof.End),
			proof.Nodes,
			true,
		)
		// Consider extracting celestia-app's namespace package. We can't use it
		// here because that would introduce a circular import.
		namespace := append([]byte{uint8(sp.NamespaceVersion)}, sp.NamespaceId...)
		valid := nmtProof.VerifyInclusion(
			appconsts.NewBaseHashFunc(),
			namespace,
			sp.Data[cursor:cursor+sharesUsed],
			sp.RowProof.RowRoots[i],
		)
		if !valid {
			return false
		}
		cursor += sharesUsed
	}
	return true
}
