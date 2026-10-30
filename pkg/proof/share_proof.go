package proof

import (
	"errors"
	"fmt"
	"math"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/go-square/v4/share"
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
	if sp.RowProof == nil {
		return errors.New("nil row proof")
	}
	if len(sp.NamespaceId) != share.NamespaceIDSize {
		return fmt.Errorf("namespace ID must be %d bytes, got %d", share.NamespaceIDSize, len(sp.NamespaceId))
	}

	if len(sp.ShareProofs) != len(sp.RowProof.RowRoots) {
		return fmt.Errorf("the number of share proofs %d must equal the number of row roots %d", len(sp.ShareProofs), len(sp.RowProof.RowRoots))
	}

	// Sum into int64 so the running total does not wrap. Also check each
	// per-proof span before adding so a single malformed entry is caught
	// before it can poison the sum.
	var numberOfSharesInProofs int64
	for _, proof := range sp.ShareProofs {
		if proof == nil {
			return errors.New("nil share proof")
		}
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

	// The data root commits to 2k row roots and 2k column roots for a k-wide
	// original square. Shares must form one contiguous range inside it.
	width := sp.RowProof.Proofs[0].Total / 4
	last := len(sp.ShareProofs) - 1
	for i, proof := range sp.ShareProofs {
		if sp.RowProof.Proofs[i].Index >= width {
			return fmt.Errorf("row %d is outside the original data square of width %d", sp.RowProof.Proofs[i].Index, width)
		}
		if int64(proof.End) > width {
			return fmt.Errorf("share proof %d ends at %d past the square width %d", i, proof.End, width)
		}
		if i > 0 && proof.Start != 0 {
			return fmt.Errorf("share proof %d must start at the beginning of its row", i)
		}
		if i < last && int64(proof.End) != width {
			return fmt.Errorf("share proof %d must end at the end of its row", i)
		}
	}

	if ok := sp.VerifyProof(); !ok {
		return errors.New("share proof failed to verify")
	}

	return nil
}

// VerifyProof checks only the NMT proofs of the shares against the row roots.
// It does not check share positions or the row proof. Use Validate for that.
func (sp ShareProof) VerifyProof() bool {
	if sp.NamespaceVersion > math.MaxUint8 || len(sp.NamespaceId) != share.NamespaceIDSize {
		return false
	}
	if sp.RowProof == nil || len(sp.RowProof.RowRoots) != len(sp.ShareProofs) {
		return false
	}
	cursor := int64(0)
	dataLen := int64(len(sp.Data))
	for i, proof := range sp.ShareProofs {
		if proof == nil || proof.Start < 0 || proof.End <= proof.Start {
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
