package types

import (
	"bytes"

	v3 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v3"
	"github.com/celestiaorg/go-square/v2/inclusion"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/celestiaorg/go-square/v2/tx"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/tendermint/tendermint/crypto/merkle"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
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
func ValidateBlobTx(txcfg client.TxEncodingConfig, bTx *tx.BlobTx, subtreeRootThreshold int, appVersion uint64) error {
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
		sizes[i] = uint32(len(pblob.Data()))
	}
	err = ValidateBlobs(bTx.Blobs...)
	if err != nil {
		return err
	}

	signer, err := sdk.AccAddressFromBech32(msgPFB.Signer)
	if err != nil {
		return err
	}
	for _, blob := range bTx.Blobs {
		// If share version is 1, assert that the signer in the blob
		// matches the signer in the msgPFB.
		if blob.ShareVersion() == share.ShareVersionOne {
			if appVersion <= v3.Version {
				return ErrUnsupportedShareVersion.Wrapf("share version %d is not supported in %d. Supported from v3 onwards", blob.ShareVersion(), appVersion)
			}
			if !bytes.Equal(blob.Signer(), signer) {
				return ErrInvalidBlobSigner.Wrapf("blob signer %s does not match msgPFB signer %s", sdk.AccAddress(blob.Signer()).String(), msgPFB.Signer)
			}
		}
	}

	// check that the sizes in the blobTx match the sizes in the msgPFB
	if !equalSlices(sizes, msgPFB.BlobSizes) {
		return ErrBlobSizeMismatch.Wrapf("actual %v declared %v", sizes, msgPFB.BlobSizes)
	}

	for i, ns := range msgPFB.Namespaces {
		msgPFBNamespace, err := share.NewNamespaceFromBytes(ns)
		if err != nil {
			return err
		}

		if !bytes.Equal(bTx.Blobs[i].Namespace().Bytes(), msgPFBNamespace.Bytes()) {
			return ErrNamespaceMismatch.Wrapf("%v %v", bTx.Blobs[i].Namespace().Bytes(), msgPFB.Namespaces[i])
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
		sharesUsed += share.SparseSharesNeeded(uint32(len(blob.Data)))
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
