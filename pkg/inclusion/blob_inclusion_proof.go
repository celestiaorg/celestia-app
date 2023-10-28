package inclusion

import (
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/blob"
	"github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/nmt"
	"github.com/tendermint/tendermint/crypto/merkle"
)

var p = nmt.Proof{}

type BlobShareCommitmentProof struct {
	// proofToSubtreeRoots are the inclusion proofs to the subtree roots
	// required to prove that some shares were included in some subtree roots
	// was included in the commitment.
	shareProofs []nmt.Proof
	// subtreeProofs prove that some subtree roots are included in a blob share
	// commitment.
	subtreeProofs []merkle.Proof

	subtreeRoots [][]byte

	// namespace is the namespace of the blob
	namespace namespace.Namespace
}

// NewBlobShareCommitmentProof returns a new blob share commitment proof using a
// blob and location of the portion of the blob that is being proved. startIndex
// and length are denominated in bytes.
func NewBlobShareCommitmentProof(blob *blob.Blob, startIndex, length int) (BlobShareCommitmentProof, error) {
	shs, err := shares.SplitBlobs(blob)
	if err != nil {
		return BlobShareCommitmentProof{}, err
	}

	subTreeWidth := SubTreeWidth(len(shs), appconsts.DefaultSubtreeRootThreshold)

	leafSets, err := subTreeLeafSets(shs)
	if err != nil {
		return BlobShareCommitmentProof{}, err
	}

	startShare, endShare := shares.ShareIndex(len(blob.Data), startIndex), shares.ShareIndex(len(blob.Data), startIndex+length)
	startLeafSet, endLeafSet := startShare/subTreeWidth, endShare/subTreeWidth

	// we want merkle proofs for each start and end leaf set, and then this could be combined with the tx itself assuming thatm we could split the tx into shares.

	// determine which subtree roots are needed to cover the share start and end

	// use the subtree root leaf sets to calculate as few proofs as possible for
	// the shares to the subtree roots. Make sure to keep the shares that are
	// required for each proof in that proof.

	// then calculate the rest of the subtree roots and the binary merkle proof to those subtree roots.

	// then write tests that make round trips

}

func (bscp *BlobShareCommitmentProof) Verify(commitment []byte, leafShares [][]shares.Share) bool {
	// verify that the share proofs are included in the commitment
	for i, shareProof := range bscp.shareProofs {
		// check the inclusion of each set of shares to their respective subtree
		// root
		if !shareProof.VerifyInclusion(
			appconsts.NewBaseHashFunc(),
			bscp.namespace.ID,
			shares.ToBytes(leafShares[i]),
			bscp.subtreeRoots[i],
		) {
			return false
		}
	}

	// verify that the subtree proofs are included in the share proofs
	for i, subtreeProof := range bscp.subtreeProofs {
		if err := subtreeProof.Verify(commitment, bscp.subtreeRoots[i]); err != nil {
			return false
		}
	}

	return true
}
