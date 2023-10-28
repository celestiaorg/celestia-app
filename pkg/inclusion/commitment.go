package inclusion

import (
	"crypto/sha256"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/blob"
	"github.com/celestiaorg/celestia-app/pkg/namespace"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	appshares "github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/nmt"
	"github.com/tendermint/tendermint/crypto/merkle"
)

// CreateCommitment generates the share commitment for a given blob.
// See [data square layout rationale] and [blob share commitment rules].
//
// [data square layout rationale]: ../../specs/src/specs/data_square_layout.md
// [blob share commitment rules]: ../../specs/src/specs/data_square_layout.md#blob-share-commitment-rules
func CreateCommitment(blob *blob.Blob) ([]byte, error) {
	if err := blob.Validate(); err != nil {
		return nil, err
	}

	shares, err := appshares.SplitBlobs(blob)
	if err != nil {
		return nil, err
	}

	// the commitment is the root of a merkle mountain range with max tree size
	// determined by the number of roots required to create a share commitment
	// over that blob. The size of the tree is only increased if the number of
	// subtree roots surpasses a constant threshold.

	subTreeRoots, err := SubtreeRoots(blob.Namespace(), shares)
	if err != nil {
		return nil, err
	}

	return merkle.HashFromByteSlices(subTreeRoots), nil
}

// SubtreeRoots returns the subtree roots given a set of shares and a namespace.
func SubtreeRoots(ns namespace.Namespace, shrs []shares.Share) ([][]byte, error) {
	leafSets, err := subTreeLeafSets(shrs)
	if err != nil {
		return nil, err
	}

	// create the commitments by pushing each leaf set onto an nmt
	subTreeRoots := make([][]byte, len(leafSets))
	for i, set := range leafSets {
		tree := nmt.New(sha256.New(), nmt.NamespaceIDSize(appns.NamespaceSize), nmt.IgnoreMaxNamespace(true))
		for _, leaf := range set {
			// the namespace must be added again here even though it is already
			// included in the leaf to ensure that the hash will match that of
			// the nmt wrapper (pkg/wrapper). Each namespace is added to keep
			// the namespace in the share, and therefore the parity data, while
			// also allowing for the manual addition of the parity namespace to
			// the parity data.
			nsLeaf := make([]byte, 0)
			nsLeaf = append(nsLeaf, ns.Bytes()...)
			nsLeaf = append(nsLeaf, leaf...)

			err = tree.Push(nsLeaf)
			if err != nil {
				return nil, err
			}
		}
		// add the root
		root, err := tree.Root()
		if err != nil {
			return nil, err
		}
		subTreeRoots[i] = root
	}

	return subTreeRoots, nil
}

// subTreeLeafSets will break the shares into nested slices of shares depending on
// the subtree width. These chunks can be used as leaves to subtreeroots.
func subTreeLeafSets(shrs []shares.Share) ([][][]byte, error) {
	subTreeWidth := SubTreeWidth(len(shrs), appconsts.DefaultSubtreeRootThreshold)
	treeSizes, err := MerkleMountainRangeSizes(uint64(len(shrs)), uint64(subTreeWidth))
	if err != nil {
		return nil, err
	}
	leafSets := make([][][]byte, len(treeSizes))
	cursor := uint64(0)
	for i, treeSize := range treeSizes {
		leafSets[i] = appshares.ToBytes(shrs[cursor : cursor+treeSize])
		cursor = cursor + treeSize
	}
	return leafSets, nil
}

// CreateCommitments is a helper function that creates a blob share commitment
// for each blob provided.
func CreateCommitments(blobs []*blob.Blob) ([][]byte, error) {
	commitments := make([][]byte, len(blobs))
	for i, blob := range blobs {
		commitment, err := CreateCommitment(blob)
		if err != nil {
			return nil, err
		}
		commitments[i] = commitment
	}
	return commitments, nil
}

// MerkleMountainRangeSizes returns the sizes (number of leaf nodes) of the
// trees in a merkle mountain range constructed for a given totalSize and
// maxTreeSize.
//
// https://docs.grin.mw/wiki/chain-state/merkle-mountain-range/
// https://github.com/opentimestamps/opentimestamps-server/blob/master/doc/merkle-mountain-range.md
func MerkleMountainRangeSizes(totalSize, maxTreeSize uint64) ([]uint64, error) {
	var treeSizes []uint64

	for totalSize != 0 {
		switch {
		case totalSize >= maxTreeSize:
			treeSizes = append(treeSizes, maxTreeSize)
			totalSize = totalSize - maxTreeSize
		case totalSize < maxTreeSize:
			treeSize, err := appshares.RoundDownPowerOfTwo(totalSize)
			if err != nil {
				return treeSizes, err
			}
			treeSizes = append(treeSizes, treeSize)
			totalSize = totalSize - treeSize
		}
	}

	return treeSizes, nil
}
