package types

import (
	"bytes"
	"runtime"
	"slices"

	"github.com/celestiaorg/go-square/v4/inclusion"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/celestiaorg/go-square/v4/tx"
	"github.com/cometbft/cometbft/crypto/merkle"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// NewV0Blob creates a new V0 Blob from a provided namespace and data.
func NewV0Blob(ns share.Namespace, data []byte) (*share.Blob, error) {
	// checks that it is a non reserved, valid namespace
	err := ValidateBlobNamespace(ns)
	if err != nil {
		return nil, err
	}

	return share.NewV0Blob(ns, data)
}

// NewV1Blob creates a new V1 Blob from the provided namespace, data and the signer
// that will pay for the blob.
func NewV1Blob(ns share.Namespace, data []byte, signer sdk.AccAddress) (*share.Blob, error) {
	err := ValidateBlobNamespace(ns)
	if err != nil {
		return nil, err
	}
	return share.NewV1Blob(ns, data, signer)
}

// ValidateBlobTx performs stateless checks on the BlobTx to ensure that the
// blobs attached to the transaction are valid.
func ValidateBlobTx(txcfg client.TxEncodingConfig, bTx *tx.BlobTx, subtreeRootThreshold int, _ uint64) error {
	msgPFB, err := ValidateBlobTxSkipCommitment(txcfg, bTx)
	if err != nil {
		return err
	}

	// verify that the commitment of the blob matches that of the msgPFB
	calculatedCommitments, err := inclusion.CreateParallelCommitments(bTx.Blobs, merkle.HashFromByteSlices, subtreeRootThreshold, runtime.NumCPU()*2)
	if err != nil {
		return ErrCalculateCommitment
	}
	for i, commitment := range msgPFB.ShareCommitments {
		if !bytes.Equal(calculatedCommitments[i], commitment) {
			return ErrInvalidShareCommitment
		}
	}

	return nil
}

// ValidateBlobTxSkipCommitment performs the same validation as ValidateBlobTx but skips
// the expensive commitment generation and verification step. This should only be used
// when the commitment validation has already been performed (e.g., in CheckTx) and
// cached for reuse in ProcessProposal.
func ValidateBlobTxSkipCommitment(txcfg client.TxEncodingConfig, bTx *tx.BlobTx) (*MsgPayForBlobs, error) {
	if bTx == nil {
		return nil, ErrNoBlobs
	}

	sdkTx, err := txcfg.TxDecoder()(bTx.Tx)
	if err != nil {
		return nil, err
	}

	// TODO: remove this check once support for multiple sdk.Msgs in a BlobTx is
	// supported.
	msgs := sdkTx.GetMsgs()
	if len(msgs) != 1 {
		return nil, ErrMultipleMsgsInBlobTx
	}
	msg := msgs[0]
	msgPFB, ok := msg.(*MsgPayForBlobs)
	if !ok {
		return nil, ErrNoPFB
	}
	err = msgPFB.ValidateBasic()
	if err != nil {
		return nil, err
	}

	// perform basic checks on the blobs
	sizes := make([]uint32, len(bTx.Blobs))
	for i, pblob := range bTx.Blobs {
		sizes[i] = uint32(len(pblob.Data()))
	}
	err = ValidateBlobs(bTx.Blobs...)
	if err != nil {
		return nil, err
	}

	signer, err := sdk.AccAddressFromBech32(msgPFB.Signer)
	if err != nil {
		return nil, err
	}
	for _, blob := range bTx.Blobs {
		// If share version is 1, assert that the signer in the blob
		// matches the signer in the msgPFB.
		if blob.ShareVersion() == share.ShareVersionOne {
			if !bytes.Equal(blob.Signer(), signer) {
				return nil, ErrInvalidBlobSigner.Wrapf("blob signer %s does not match msgPFB signer %s", sdk.AccAddress(blob.Signer()).String(), msgPFB.Signer)
			}
		}
	}

	// check that the sizes in the blobTx match the sizes in the msgPFB
	if !slices.Equal(sizes, msgPFB.BlobSizes) {
		return nil, ErrBlobSizeMismatch.Wrapf("actual %v declared %v", sizes, msgPFB.BlobSizes)
	}

	for i, ns := range msgPFB.Namespaces {
		msgPFBNamespace, err := share.NewNamespaceFromBytes(ns)
		if err != nil {
			return nil, err
		}

		if !bytes.Equal(bTx.Blobs[i].Namespace().Bytes(), msgPFBNamespace.Bytes()) {
			return nil, ErrNamespaceMismatch.Wrapf("%v %v", bTx.Blobs[i].Namespace().Bytes(), msgPFB.Namespaces[i])
		}
	}

	return msgPFB, nil
}
