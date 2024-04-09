package types

import (
	"crypto/sha256"
	fmt "fmt"

	"cosmossdk.io/errors"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	appshares "github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/nmt"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/migrations/legacytx"
	auth "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/tendermint/tendermint/crypto/merkle"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
	"golang.org/x/exp/slices"
)

const (
	URLMsgPayForBlobs = "/celestia.blob.v1.MsgPayForBlobs"
	ShareSize         = appconsts.ShareSize

	// PFBGasFixedCost is a rough estimate for the "fixed cost" in the gas cost
	// formula: gas cost = gas per byte * bytes per share * shares occupied by
	// blob + "fixed cost". In this context, "fixed cost" accounts for the gas
	// consumed by operations outside the blob's GasToConsume function (i.e.
	// signature verification, tx size, read access to accounts).
	//
	// Since the gas cost of these operations is not easy to calculate, linear
	// regression was performed on a set of observed data points to derive an
	// approximate formula for gas cost. Assuming gas per byte = 8 and bytes per
	// share = 512, we can solve for "fixed cost" and arrive at 65,000. gas cost
	// = 8 * 512 * number of shares occupied by the blob + 65,000 has a
	// correlation coefficient of 0.996. To be conservative, we round up "fixed
	// cost" to 75,000 because the first tx always takes up 10,000 more gas than
	// subsequent txs.
	PFBGasFixedCost = 75000

	// BytesPerBlobInfo is a rough estimation for the amount of extra bytes in
	// information a blob adds to the size of the underlying transaction.
	BytesPerBlobInfo = 70
)

// MsgPayForBlobs implements the `LegacyMsg` interface.
// See: https://github.com/cosmos/cosmos-sdk/blob/v0.46.15/docs/building-modules/messages-and-queries.md#legacy-amino-legacymsgs
var _ legacytx.LegacyMsg = &MsgPayForBlobs{}

func NewMsgPayForBlobs(signer string, blobs ...*Blob) (*MsgPayForBlobs, error) {
	err := ValidateBlobs(blobs...)
	if err != nil {
		return nil, err
	}
	commitments, err := CreateCommitments(blobs)
	if err != nil {
		return nil, err
	}

	namespaceVersions, namespaceIDs, sizes, shareVersions := extractBlobComponents(blobs)
	namespaces := []appns.Namespace{}
	for i := range namespaceVersions {
		namespace, err := appns.New(uint8(namespaceVersions[i]), namespaceIDs[i])
		if err != nil {
			return nil, err
		}
		namespaces = append(namespaces, namespace)
	}

	msg := &MsgPayForBlobs{
		Signer:           signer,
		Namespaces:       namespacesToBytes(namespaces),
		ShareCommitments: commitments,
		BlobSizes:        sizes,
		ShareVersions:    shareVersions,
	}

	return msg, msg.ValidateBasic()
}

func namespacesToBytes(namespaces []appns.Namespace) (result [][]byte) {
	for _, namespace := range namespaces {
		result = append(result, namespace.Bytes())
	}
	return result
}

// Route fulfills the legacytx.LegacyMsg interface
func (msg *MsgPayForBlobs) Route() string { return RouterKey }

// Type fulfills the legacytx.LegacyMsg interface
func (msg *MsgPayForBlobs) Type() string {
	return URLMsgPayForBlobs
}

// ValidateBasic fulfills the sdk.Msg interface by performing stateless
// validity checks on the msg that also don't require having the actual blob(s)
func (msg *MsgPayForBlobs) ValidateBasic() error {
	if len(msg.Namespaces) == 0 {
		return ErrNoNamespaces
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

	if len(msg.Namespaces) != len(msg.ShareVersions) || len(msg.Namespaces) != len(msg.BlobSizes) || len(msg.Namespaces) != len(msg.ShareCommitments) {
		return ErrMismatchedNumberOfPFBComponent.Wrapf(
			"namespaces %d blob sizes %d share versions %d share commitments %d",
			len(msg.Namespaces), len(msg.BlobSizes), len(msg.ShareVersions), len(msg.ShareCommitments),
		)
	}

	for _, namespace := range msg.Namespaces {
		ns, err := appns.From(namespace)
		if err != nil {
			return errors.Wrap(ErrInvalidNamespace, err.Error())
		}
		err = ValidateBlobNamespace(ns)
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
		if len(commitment) != appconsts.HashLength() {
			return ErrInvalidShareCommitment
		}
	}

	return nil
}

func (msg *MsgPayForBlobs) Gas(gasPerByte uint32) uint64 {
	return GasToConsume(msg.BlobSizes, gasPerByte)
}

// GasToConsume works out the extra gas charged to pay for a set of blobs in a PFB.
// Note that tranasctions will incur other gas costs, such as the signature verification
// and reads to the user's account.
func GasToConsume(blobSizes []uint32, gasPerByte uint32) uint64 {
	var totalSharesUsed uint64
	for _, size := range blobSizes {
		totalSharesUsed += uint64(appshares.SparseSharesNeeded(size))
	}

	return totalSharesUsed * appconsts.ShareSize * uint64(gasPerByte)
}

// EstimateGas estimates the total gas required to pay for a set of blobs in a PFB.
// It is based on a linear model that is dependent on the governance parameters:
// gasPerByte and txSizeCost. It assumes other variables are constant. This includes
// assuming the PFB is the only message in the transaction.
func EstimateGas(blobSizes []uint32, gasPerByte uint32, txSizeCost uint64) uint64 {
	return GasToConsume(blobSizes, gasPerByte) + (txSizeCost * BytesPerBlobInfo * uint64(len(blobSizes))) + PFBGasFixedCost
}

// DefaultEstimateGas runs EstimateGas with the system defaults. The network may change these values
// through governance, thus this function should predominantly be used in testing.
func DefaultEstimateGas(blobSizes []uint32) uint64 {
	return EstimateGas(blobSizes, appconsts.DefaultGasPerBlobByte, auth.DefaultTxSizeCostPerByte)
}

// ValidateBlobNamespace returns an error if the provided namespace is an
// invalid user-specifiable blob namespace (e.g. reserved, parity shares, or
// tail padding).
func ValidateBlobNamespace(ns appns.Namespace) error {
	if ns.IsReserved() {
		return ErrReservedNamespace
	}

	if !slices.Contains(appns.SupportedBlobNamespaceVersions, ns.Version) {
		return ErrInvalidNamespaceVersion
	}

	return nil
}

// GetSignBytes fulfills the legacytx.LegacyMsg interface by returning a deterministic set
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

// CreateCommitment generates the share commitment for a given blob.
// See [data square layout rationale] and [blob share commitment rules].
//
// [data square layout rationale]: ../../specs/src/specs/data_square_layout.md
// [blob share commitment rules]: ../../specs/src/specs/data_square_layout.md#blob-share-commitment-rules
func CreateCommitment(blob *Blob) ([]byte, error) {
	coreblob := coretypes.Blob{
		NamespaceID:      blob.NamespaceId,
		Data:             blob.Data,
		ShareVersion:     uint8(blob.ShareVersion),
		NamespaceVersion: uint8(blob.NamespaceVersion),
	}

	shares, err := appshares.SplitBlobs(coreblob)
	if err != nil {
		return nil, err
	}

	// the commitment is the root of a merkle mountain range with max tree size
	// determined by the number of roots required to create a share commitment
	// over that blob. The size of the tree is only increased if the number of
	// subtree roots surpasses a constant threshold.
	subTreeWidth := appshares.SubTreeWidth(len(shares), appconsts.DefaultSubtreeRootThreshold)
	treeSizes, err := merkleMountainRangeSizes(uint64(len(shares)), uint64(subTreeWidth))
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
		tree := nmt.New(sha256.New(), nmt.NamespaceIDSize(appns.NamespaceSize), nmt.IgnoreMaxNamespace(true))
		for _, leaf := range set {
			namespace, err := appns.New(uint8(blob.NamespaceVersion), blob.NamespaceId)
			if err != nil {
				return nil, err
			}
			// the namespace must be added again here even though it is already
			// included in the leaf to ensure that the hash will match that of
			// the nmt wrapper (pkg/wrapper). Each namespace is added to keep
			// the namespace in the share, and therefore the parity data, while
			// also allowing for the manual addition of the parity namespace to
			// the parity data.
			nsLeaf := make([]byte, 0)
			nsLeaf = append(nsLeaf, namespace.Bytes()...)
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

// ValidateBlobs performs basic checks over the components of one or more PFBs.
func ValidateBlobs(blobs ...*Blob) error {
	if len(blobs) == 0 {
		return ErrNoBlobs
	}

	for _, blob := range blobs {
		if blob.NamespaceVersion > appconsts.NamespaceVersionMaxValue {
			return fmt.Errorf("namespace version %d is too large", blob.NamespaceVersion)
		}
		ns, err := appns.New(uint8(blob.NamespaceVersion), blob.NamespaceId)
		if err != nil {
			return err
		}
		err = ValidateBlobNamespace(ns)
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

// extractBlobComponents separates and returns the components of a slice of
// blobs.
func extractBlobComponents(pblobs []*tmproto.Blob) (namespaceVersions []uint32, namespaceIDs [][]byte, sizes []uint32, shareVersions []uint32) {
	namespaceVersions = make([]uint32, len(pblobs))
	namespaceIDs = make([][]byte, len(pblobs))
	sizes = make([]uint32, len(pblobs))
	shareVersions = make([]uint32, len(pblobs))

	for i, pblob := range pblobs {
		namespaceVersions[i] = pblob.NamespaceVersion
		namespaceIDs[i] = pblob.NamespaceId
		sizes[i] = uint32(len(pblob.Data))
		shareVersions[i] = pblob.ShareVersion
	}

	return namespaceVersions, namespaceIDs, sizes, shareVersions
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
