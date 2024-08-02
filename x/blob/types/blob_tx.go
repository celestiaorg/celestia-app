package types

import (
	"bytes"

	"github.com/celestiaorg/go-square/blob"
	"github.com/celestiaorg/go-square/inclusion"
	appns "github.com/celestiaorg/go-square/namespace"
	shares "github.com/celestiaorg/go-square/shares"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/tendermint/tendermint/crypto/merkle"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

// NewBlob creates a new coretypes.Blob from the provided data after performing
// basic stateless checks over it.
func NewBlob(ns appns.Namespace, data []byte, shareVersion uint8) (*blob.Blob, error) {
	err := ValidateBlobNamespace(ns)
	if err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return nil, ErrZeroBlobSize
	}

	return &blob.Blob{
		NamespaceId:      ns.ID,
		Data:             data,
		ShareVersion:     uint32(shareVersion),
		NamespaceVersion: uint32(ns.Version),
	}, nil
}

// ValidateBlobTx performs stateless checks on the BlobTx to ensure that the
// blobs attached to the transaction are valid.
func ValidateBlobTx(txcfg client.TxEncodingConfig, bTx *blob.BlobTx, subtreeRootThreshold int) error {
	if bTx == nil {
		return ErrNoBlobs
	}

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
	msgPFB, ok := msg.(*MsgPayForBlobs)
	if !ok {
		return ErrNoPFB
	}
	err = msgPFB.ValidateBasic()
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

	// check that the sizes in the blobTx match the sizes in the msgPFB
	if !equalSlices(sizes, msgPFB.BlobSizes) {
		return ErrBlobSizeMismatch.Wrapf("actual %v declared %v", sizes, msgPFB.BlobSizes)
	}

	for i, ns := range msgPFB.Namespaces {
		msgPFBNamespace, err := appns.From(ns)
		if err != nil {
			return err
		}

		// this not only checks that the pfb namespaces match the ones in the blobs
		// but that the namespace version and namespace id are valid
		blobNamespace, err := appns.New(uint8(bTx.Blobs[i].NamespaceVersion), bTx.Blobs[i].NamespaceId)
		if err != nil {
			return err
		}

		if !bytes.Equal(blobNamespace.Bytes(), msgPFBNamespace.Bytes()) {
			return ErrNamespaceMismatch.Wrapf("%v %v", blobNamespace.Bytes(), msgPFB.Namespaces[i])
		}
	}

	// verify that the commitment of the blob matches that of the msgPFB
	for i, commitment := range msgPFB.ShareCommitments {
		calculatedCommit, err := inclusion.CreateCommitment(bTx.Blobs[i], merkle.HashFromByteSlices, subtreeRootThreshold)
		if err != nil {
			return ErrCalculateCommitment
		}
		if !bytes.Equal(calculatedCommit, commitment) {
			return ErrInvalidShareCommitment
		}
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
