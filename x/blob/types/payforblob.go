package types

import (
	"bytes"
	"crypto/sha256"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	appshares "github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/nmt"
	"github.com/celestiaorg/nmt/namespace"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/tendermint/tendermint/crypto/merkle"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
	"golang.org/x/exp/constraints"
	"golang.org/x/exp/slices"
)

const (
	URLMsgPayForBlobs = "/celestia.blob.v1.MsgPayForBlobs"
	ShareSize         = appconsts.ShareSize
	SquareSize        = appconsts.DefaultMaxSquareSize
	NamespaceIDSize   = appconsts.NamespaceSize
)

var _ sdk.Msg = &MsgPayForBlobs{}

func NewMsgPayForBlobs(signer string, blobs ...*Blob) (*MsgPayForBlobs, error) {
	nsIDs, sizes, versions := extractBlobComponents(blobs)
	err := ValidateBlobs(blobs...)
	if err != nil {
		return nil, err
	}

	commitments, err := CreateCommitments(blobs)
	if err != nil {
		return nil, err
	}

	msg := &MsgPayForBlobs{
		Signer:           signer,
		NamespaceIds:     nsIDs,
		ShareCommitments: commitments,
		BlobSizes:        sizes,
		ShareVersions:    versions,
	}

	return msg, msg.ValidateBasic()
}

// Route fulfills the sdk.Msg interface
func (msg *MsgPayForBlobs) Route() string { return RouterKey }

// Type fulfills the sdk.Msg interface
func (msg *MsgPayForBlobs) Type() string {
	return URLMsgPayForBlobs
}

// ValidateBasic fulfills the sdk.Msg interface by performing stateless
// validity checks on the msg that also don't require having the actual blob(s)
func (msg *MsgPayForBlobs) ValidateBasic() error {
	if len(msg.NamespaceIds) == 0 {
		return ErrNoNamespaceIds
	}

	if len(msg.ShareVersions) == 0 {
		return ErrNoShareVersions
	}

	if len(msg.BlobSizes) == 0 {
		return ErrNoBlobSizes
	}

	if len(msg.ShareCommitments) == 0 {
		return ErrNoShareCommitments
	}

	if len(msg.NamespaceIds) != len(msg.ShareVersions) || len(msg.NamespaceIds) != len(msg.BlobSizes) || len(msg.NamespaceIds) != len(msg.ShareCommitments) {
		return ErrMismatchedNumberOfPFBComponent.Wrapf(
			"namespaces %d blob sizes %d versions %d share commitments %d",
			len(msg.NamespaceIds), len(msg.BlobSizes), len(msg.ShareVersions), len(msg.ShareCommitments),
		)
	}

	for _, ns := range msg.NamespaceIds {
		err := ValidateBlobNamespaceID(ns)
		if err != nil {
			return err
		}
	}

	for _, v := range msg.ShareVersions {
		if v != uint32(appconsts.ShareVersionZero) {
			return ErrUnsupportedShareVersion
		}
	}

	_, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		return err
	}

	for _, commitment := range msg.ShareCommitments {
		if len(commitment) == 0 {
			return ErrEmptyShareCommitment
		}
	}

	return nil
}

// GetSignBytes fulfills the sdk.Msg interface by returning a deterministic set
// of bytes to sign over
func (msg *MsgPayForBlobs) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg))
}

// GetSigners fulfills the sdk.Msg interface by returning the signer's address
func (msg *MsgPayForBlobs) GetSigners() []sdk.AccAddress {
	address, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{address}
}

// CreateCommitment generates the commitment bytes for a given namespace,
// blobData, and shareVersion using a namespace merkle tree and the rules
// described at [Message layout rationale] and [Non-interactive default rules].
//
// [Message layout rationale]: https://github.com/celestiaorg/celestia-specs/blob/e59efd63a2165866584833e91e1cb8a6ed8c8203/src/rationale/message_block_layout.md?plain=1#L12
// [Non-interactive default rules]: https://github.com/celestiaorg/celestia-specs/blob/e59efd63a2165866584833e91e1cb8a6ed8c8203/src/rationale/message_block_layout.md?plain=1#L36
func CreateCommitment(blob *Blob) ([]byte, error) {
	coreblob := coretypes.Blob{
		NamespaceID:  blob.NamespaceId,
		Data:         blob.Data,
		ShareVersion: uint8(blob.ShareVersion),
	}

	// split into shares that are length delimited and include the namespace in
	// each share
	shares, err := appshares.SplitBlobs(0, nil, []coretypes.Blob{coreblob}, false)
	if err != nil {
		return nil, err
	}

	// the commitment is the root of a merkle mountain range with max tree size
	// equal to the minimum square size the blob can be included in. See
	// https://github.com/celestiaorg/celestia-app/blob/fbfbf111bcaa056e53b0bc54d327587dee11a945/docs/architecture/adr-008-blocksize-independent-commitment.md
	minSquareSize := BlobMinSquareSize(len(blob.Data))
	treeSizes, err := merkleMountainRangeSizes(uint64(len(shares)), uint64(minSquareSize))
	if err != nil {
		return nil, err
	}
	leafSets := make([][][]byte, len(treeSizes))
	cursor := uint64(0)
	for i, treeSize := range treeSizes {
		leafSets[i] = appshares.ToBytes(shares[cursor : cursor+treeSize])
		cursor = cursor + treeSize
	}

	// create the commitments by pushing each leaf set onto an nmt
	subTreeRoots := make([][]byte, len(leafSets))
	for i, set := range leafSets {
		// create the nmt todo(evan) use nmt wrapper
		tree := nmt.New(sha256.New())
		for _, leaf := range set {
			// the namespace must be added again here even though it is already
			// included in the leaf to ensure that the hash will match that of
			// the nmt wrapper (pkg/wrapper). Each namespace is added to keep
			// the namespace in the share, and therefore the parity data, while
			// also allowing for the manual addition of the parity namespace to
			// the parity data.
			nsLeaf := append(make([]byte, 0), append(blob.NamespaceId, leaf...)...)
			err := tree.Push(nsLeaf)
			if err != nil {
				return nil, err
			}
		}
		// add the root
		subTreeRoots[i] = tree.Root()
	}
	return merkle.HashFromByteSlices(subTreeRoots), nil
}

func CreateCommitments(blobs []*Blob) ([][]byte, error) {
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

// ValidatePFBComponents performs basic checks over the components of one or more PFBs.
func ValidateBlobs(blobs ...*Blob) error {
	if len(blobs) == 0 {
		return ErrNoBlobs
	}

	for _, blob := range blobs {
		err := ValidateBlobNamespaceID(blob.NamespaceId)
		if err != nil {
			return err
		}

		if len(blob.Data) == 0 {
			return ErrZeroBlobSize
		}

		if !slices.Contains(appconsts.SupportedShareVersions, uint8(blob.ShareVersion)) {
			return ErrUnsupportedShareVersion
		}
	}

	return nil
}

// ValidateBlobNamespaceID returns an error if the provided namespace.ID is an invalid or reserved namespace id.
func ValidateBlobNamespaceID(ns namespace.ID) error {
	// ensure that the namespace id is of length == NamespaceIDSize
	if nsLen := len(ns); nsLen != NamespaceIDSize {
		return ErrInvalidNamespaceLen.Wrapf("got: %d want: %d",
			nsLen,
			NamespaceIDSize,
		)
	}
	// ensure that a reserved namespace is not used
	if bytes.Compare(ns, appconsts.MaxReservedNamespace) < 1 {
		return ErrReservedNamespace.Wrapf("got namespace: %x, want: > %x", ns, appconsts.MaxReservedNamespace)
	}

	// ensure that ParitySharesNamespaceID is not used
	if bytes.Equal(ns, appconsts.ParitySharesNamespaceID) {
		return ErrParitySharesNamespace
	}

	// ensure that TailPaddingNamespaceID is not used
	if bytes.Equal(ns, appconsts.TailPaddingNamespaceID) {
		return ErrTailPaddingNamespace
	}

	return nil
}

// extractBlobComponents separates and returns the components of a slice of
// blobs in order of blobs of data, their namespaces, their sizes, and their share
// versions.
func extractBlobComponents(pblobs []*tmproto.Blob) (nsIDs [][]byte, sizes []uint32, versions []uint32) {
	nsIDs = make([][]byte, len(pblobs))
	sizes = make([]uint32, len(pblobs))
	versions = make([]uint32, len(pblobs))

	for i, pblob := range pblobs {
		sizes[i] = uint32(len(pblob.Data))
		nsIDs[i] = pblob.NamespaceId
		versions[i] = pblob.ShareVersion
	}

	return nsIDs, sizes, versions
}

// BlobMinSquareSize returns the minimum square size that blobSize can be included
// in. The returned square size does not account for the associated transaction
// shares or non-interactive defaults, so it is a minimum.
func BlobMinSquareSize[T constraints.Integer](blobSize T) T {
	shareCount := appshares.SparseSharesNeeded(uint32(blobSize))
	return T(appshares.MinSquareSize(shareCount))
}

// merkleMountainRangeSizes returns the sizes (number of leaf nodes) of the
// trees in a merkle mountain range constructed for a given totalSize and
// maxTreeSize.
//
// https://docs.grin.mw/wiki/chain-state/merkle-mountain-range/
// https://github.com/opentimestamps/opentimestamps-server/blob/master/doc/merkle-mountain-range.md
// TODO: potentially rename function because this doesn't return heights
func merkleMountainRangeSizes(totalSize, maxTreeSize uint64) ([]uint64, error) {
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
