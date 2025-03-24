package types

import (
	"bytes"
	fmt "fmt"
	"slices"

	"cosmossdk.io/errors"
	"github.com/cometbft/cometbft/crypto/merkle"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/celestiaorg/go-square/v2/inclusion"
	"github.com/celestiaorg/go-square/v2/share"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
)

const (
	URLMsgPayForBlobs = "/celestia.blob.v1.MsgPayForBlobs"

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

func NewMsgPayForBlobs(signer string, _ uint64, blobs ...*share.Blob) (*MsgPayForBlobs, error) {
	err := ValidateBlobs(blobs...)
	if err != nil {
		return nil, err
	}

	signerBytes, err := sdk.AccAddressFromBech32(signer)
	if err != nil {
		return nil, err
	}

	err = ValidateBlobShareVersion(signerBytes, blobs...)
	if err != nil {
		return nil, err
	}

	commitments, err := inclusion.CreateCommitments(blobs, merkle.HashFromByteSlices, appconsts.SubtreeRootThreshold)
	if err != nil {
		return nil, fmt.Errorf("creating commitments: %w", err)
	}

	namespaces, sizes, shareVersions := ExtractBlobComponents(blobs)

	msg := &MsgPayForBlobs{
		Signer:           signer,
		Namespaces:       namespacesToBytes(namespaces),
		ShareCommitments: commitments,
		BlobSizes:        sizes,
		ShareVersions:    shareVersions,
	}

	return msg, msg.ValidateBasic()
}

func namespacesToBytes(namespaces []share.Namespace) (result [][]byte) {
	for _, namespace := range namespaces {
		result = append(result, namespace.Bytes())
	}
	return result
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
		ns, err := share.NewNamespaceFromBytes(namespace)
		if err != nil {
			return errors.Wrap(ErrInvalidNamespace, err.Error())
		}
		err = ValidateBlobNamespace(ns)
		if err != nil {
			return err
		}
	}

	for _, v := range msg.ShareVersions {
		if v != uint32(share.ShareVersionZero) && v != uint32(share.ShareVersionOne) {
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
// Note that transactions will incur other gas costs, such as the signature verification
// and reads to the user's account.
func GasToConsume(blobSizes []uint32, gasPerByte uint32) uint64 {
	var totalSharesUsed uint64
	for _, size := range blobSizes {
		totalSharesUsed += uint64(share.SparseSharesNeeded(size))
	}

	return totalSharesUsed * share.ShareSize * uint64(gasPerByte)
}

// EstimateGas estimates the total gas required to pay for a set of blobs in a PFB.
// It is based on a linear model that is dependent on the governance parameters:
// gasPerByte and txSizeCost. It assumes other variables are constant. This includes
// assuming the PFB is the only message in the transaction.
func EstimateGas(blobSizes []uint32, gasPerByte uint32, txSizeCost uint64) uint64 {
	return GasToConsume(blobSizes, gasPerByte) + (txSizeCost * BytesPerBlobInfo * uint64(len(blobSizes))) + PFBGasFixedCost
}

// DefaultEstimateGas runs EstimateGas with the system defaults.
func DefaultEstimateGas(blobSizes []uint32) uint64 {
	return EstimateGas(blobSizes, appconsts.DefaultGasPerBlobByte, appconsts.DefaultTxSizeCostPerByte)
}

// ValidateBlobNamespace returns an error if the provided namespace is an
// invalid user-specifiable blob namespace (e.g. reserved, parity shares, or
// tail padding).
func ValidateBlobNamespace(ns share.Namespace) error {
	if ns.IsReserved() {
		return ErrReservedNamespace
	}

	if !slices.Contains(share.SupportedBlobNamespaceVersions, ns.Version()) {
		return ErrInvalidNamespaceVersion
	}

	return nil
}

// ValidateBlobs performs checks that each blob is non-empty and has a valid namespace.
// Other checks are done in the construction of the Blob.
func ValidateBlobs(blobs ...*share.Blob) error {
	if len(blobs) == 0 {
		return ErrNoBlobs
	}

	for _, blob := range blobs {
		if blob.IsEmpty() {
			return ErrZeroBlobSize
		}

		err := ValidateBlobNamespace(blob.Namespace())
		if err != nil {
			return err
		}
	}

	return nil
}

// ValidateBlobShareVersion validates any share version specific rules
func ValidateBlobShareVersion(signer sdk.AccAddress, blobs ...*share.Blob) error {
	for _, blob := range blobs {
		if blob.ShareVersion() == share.ShareVersionOne && !bytes.Equal(blob.Signer(), []byte(signer)) {
			return ErrInvalidBlobSigner.Wrapf("blob signer %X does not match msgPFB signer %X", blob.Signer(), signer)
		}
	}
	return nil
}

// ExtractBlobComponents separates and returns the components of a slice of
// blobs.
func ExtractBlobComponents(pblobs []*share.Blob) (namespaces []share.Namespace, sizes, shareVersions []uint32) {
	namespaces = make([]share.Namespace, len(pblobs))
	sizes = make([]uint32, len(pblobs))
	shareVersions = make([]uint32, len(pblobs))

	for i, pblob := range pblobs {
		namespaces[i] = pblob.Namespace()
		sizes[i] = uint32(len(pblob.Data()))
		shareVersions[i] = uint32(pblob.ShareVersion())
	}

	return namespaces, sizes, shareVersions
}
