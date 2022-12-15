package types

import (
	"bytes"
	"crypto/sha256"
	"math"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	appshares "github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/nmt"
	"github.com/celestiaorg/nmt/namespace"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/tendermint/tendermint/crypto/merkle"
	coretypes "github.com/tendermint/tendermint/types"
	"golang.org/x/exp/constraints"
)

const (
	URLBlobTx        = "/blob.BlobTx"
	URLMsgPayForBlob = "/blob.MsgPayForBlob"
	ShareSize        = appconsts.ShareSize
	SquareSize       = appconsts.DefaultMaxSquareSize
	NamespaceIDSize  = appconsts.NamespaceSize
)

var _ sdk.Msg = &MsgPayForBlob{}

func NewMsgPayForBlob(signer string, nid namespace.ID, blob []byte) (*MsgPayForBlob, error) {
	commitment, err := CreateCommitment(nid, blob, appconsts.ShareVersionZero)
	if err != nil {
		return nil, err
	}
	if len(blob) == 0 {
		return nil, ErrZeroBlobSize
	}
	msg := &MsgPayForBlob{
		Signer:          signer,
		NamespaceId:     nid,
		ShareCommitment: commitment,
		BlobSize:        uint64(len(blob)),
	}
	return msg, msg.ValidateBasic()
}

// Route fulfills the sdk.Msg interface
func (msg *MsgPayForBlob) Route() string { return RouterKey }

// Type fulfills the sdk.Msg interface
func (msg *MsgPayForBlob) Type() string {
	return URLMsgPayForBlob
}

// ValidateBasic fulfills the sdk.Msg interface by performing stateless
// validity checks on the msg that also don't require having the actual blob
func (msg *MsgPayForBlob) ValidateBasic() error {
	if err := ValidateBlobNamespaceID(msg.GetNamespaceId()); err != nil {
		return err
	}

	_, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		return err
	}

	if len(msg.ShareCommitment) == 0 {
		return ErrEmptyShareCommitment
	}

	return nil
}

// GetSignBytes fulfills the sdk.Msg interface by returning a deterministic set
// of bytes to sign over
func (msg *MsgPayForBlob) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg))
}

// GetSigners fulfills the sdk.Msg interface by returning the signer's address
func (msg *MsgPayForBlob) GetSigners() []sdk.AccAddress {
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
func CreateCommitment(namespace []byte, blobData []byte, shareVersion uint8) ([]byte, error) {
	blob := coretypes.Blob{
		NamespaceID:  namespace,
		Data:         blobData,
		ShareVersion: shareVersion,
	}

	// split into shares that are length delimited and include the namespace in
	// each share
	shares, err := appshares.SplitBlobs(0, nil, []coretypes.Blob{blob}, false)
	if err != nil {
		return nil, err
	}

	// the commitment is the root of a merkle mountain range with max tree size
	// equal to the minimum square size the blob can be included in. See
	// https://github.com/celestiaorg/celestia-app/blob/fbfbf111bcaa056e53b0bc54d327587dee11a945/docs/architecture/adr-008-blocksize-independent-commitment.md
	minSquareSize := BlobMinSquareSize(len(blobData))
	treeSizes := merkleMountainRangeSizes(uint64(len(shares)), uint64(minSquareSize))
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
			nsLeaf := append(make([]byte, 0), append(namespace, leaf...)...)
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

// BlobMinSquareSize returns the minimum square size that blobSize can be included
// in. The returned square size does not account for the associated transaction
// shares or non-interactive defaults, so it is a minimum.
func BlobMinSquareSize[T constraints.Integer](blobSize T) T {
	shareCount := appshares.BlobSharesUsed(int(blobSize))
	return T(MinSquareSize(shareCount))
}

// MinSquareSize returns the minimum square size that can contain shareCount
// number of shares.
func MinSquareSize[T constraints.Integer](shareCount T) T {
	return T(appshares.RoundUpPowerOfTwo(uint64(math.Ceil(math.Sqrt(float64(shareCount))))))
}

// merkleMountainRangeSizes returns the sizes (number of leaf nodes) of the
// trees in a merkle mountain range constructed for a given totalSize and
// maxTreeSize.
//
// https://docs.grin.mw/wiki/chain-state/merkle-mountain-range/
// https://github.com/opentimestamps/opentimestamps-server/blob/master/doc/merkle-mountain-range.md
// TODO: potentially rename function because this doesn't return heights
func merkleMountainRangeSizes(totalSize, maxTreeSize uint64) []uint64 {
	var treeSizes []uint64

	for totalSize != 0 {
		switch {
		case totalSize >= maxTreeSize:
			treeSizes = append(treeSizes, maxTreeSize)
			totalSize = totalSize - maxTreeSize
		case totalSize < maxTreeSize:
			treeSize := appshares.RoundDownPowerOfTwo(totalSize)
			treeSizes = append(treeSizes, treeSize)
			totalSize = totalSize - treeSize
		}
	}

	return treeSizes
}
