package types

import (
	"bytes"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	shares "github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/cosmos/cosmos-sdk/client"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

// Blob wraps the tendermint type so that users can simply import this one.
type Blob = tmproto.Blob

// NewBlob creates a new coretypes.Blob from the provided data after performing
// basic stateless checks over it.
func NewBlob(ns namespace.ID, blob []byte) (*Blob, error) {
	err := ValidateBlobNamespaceID(ns)
	if err != nil {
		return nil, err
	}

	if len(blob) == 0 {
		return nil, ErrZeroBlobSize
	}

	return &tmproto.Blob{
		NamespaceId:  ns,
		Data:         blob,
		ShareVersion: uint32(appconsts.DefaultShareVersion),
	}, nil
}

// ValidateBlobTx performs stateless checks on the BlobTx to ensure that the
// blobs attached to the transaction are valid.
func ValidateBlobTx(txcfg client.TxEncodingConfig, bTx tmproto.BlobTx) error {
	sdkTx, err := txcfg.TxDecoder()(bTx.Tx)
	if err != nil {
		return err
	}

	// TODO: remove this check once support for multiple sdk.Msgs in a BlobTx is
	// supported.
	msgs := sdkTx.GetMsgs()
	if len(msgs) != 1 {
		return ErrMultipleMsgsInBlobTx
	}
	msg := msgs[0]
	pfb, ok := msg.(*MsgPayForBlob)
	if !ok {
		return ErrNoPFB
	}
	err = pfb.ValidateBasic()
	if err != nil {
		return err
	}

	// perform basic checks on the blobs
	sizes := make([]uint32, len(bTx.Blobs))
	for i, pblob := range bTx.Blobs {
		sizes[i] = uint32(len(pblob.Data))
	}
	err = ValidateBlobs(bTx.Blobs...)
	if err != nil {
		return err
	}

	// check that the info in the pfb matches that in the blobs
	if !equalSlices(sizes, pfb.BlobSizes) {
		return ErrBlobSizeMismatch.Wrapf("actual %v declared %v", sizes, pfb.BlobSizes)
	}

	for i := range pfb.NamespaceIds {
		// check that the metadata matches
		if !bytes.Equal(bTx.Blobs[i].NamespaceId, pfb.NamespaceIds[i]) {
			return ErrNamespaceMismatch.Wrapf("%v %v", bTx.Blobs[i].NamespaceId, pfb.NamespaceIds[i])
		}
	}

	// verify that the commitment of the blob matches that of the PFB
	calculatedCommit, err := CreateMultiShareCommitment(bTx.Blobs...)
	if err != nil {
		return ErrCalculateCommit
	}
	if !bytes.Equal(calculatedCommit, pfb.ShareCommitment) {
		return ErrInvalidShareCommit
	}

	return nil
}

func BlobTxSharesUsed(btx tmproto.BlobTx) int {
	sharesUsed := 0
	for _, blob := range btx.Blobs {
		sharesUsed += shares.SparseSharesNeeded(uint32(len(blob.Data)))
	}
	return sharesUsed
}

func equalSlices[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
