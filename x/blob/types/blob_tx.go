package types

import (
	"bytes"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
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
	// temporary check that we will remove when we support multiple blobs per PFB
	if 1 != len(bTx.Blobs) {
		return ErrMismatchedNumberOfPFBorBlob
	}
	// todo: modify this to support multiple messages per PFB
	blob := bTx.Blobs[0]

	err = pfb.ValidateBasic()
	if err != nil {
		return err
	}

	// check that the metadata matches
	if !bytes.Equal(blob.NamespaceId, pfb.NamespaceId) {
		return ErrNamespaceMismatch
	}

	if pfb.BlobSize != uint32(len(blob.Data)) {
		return ErrDeclaredActualDataSizeMismatch.Wrapf(
			"declared: %d vs actual: %d",
			pfb.BlobSize,
			len(blob.Data),
		)
	}

	// verify that the commitment of the blob matches that of the PFB
	calculatedCommit, err := CreateMultiShareCommitment(blob)
	if err != nil {
		return ErrCalculateCommit
	}
	if !bytes.Equal(calculatedCommit, pfb.ShareCommitment) {
		return ErrInvalidShareCommit
	}

	return nil
}
