package types

import (
	"bytes"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	shares "github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/cosmos/cosmos-sdk/client"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

// ProcessedBlobTx caches the unmarshalled result of the BlobTx
type ProcessedBlobTx struct {
	Blobs []*tmproto.Blob
	Tx    []byte
}

// NewBlob creates a new coretypes.Blob from the provided data after performing
// basic stateless checks over it.
func NewBlob(ns namespace.ID, blob []byte) (*tmproto.Blob, error) {
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
		ShareVersion: uint32(appconsts.ShareVersionZero),
	}, nil
}

// ProcessBlobTx performs stateless checks on the BlobTx to ensure that the
// blobs attached to the transaction are valid. During this process, it
// separates the blobs from the MsgPayForBlob, which are returned in the
// ProcessedBlobTx.
func ProcessBlobTx(txcfg client.TxEncodingConfig, bTx tmproto.BlobTx) (ProcessedBlobTx, error) {
	sdkTx, err := txcfg.TxDecoder()(bTx.Tx)
	if err != nil {
		return ProcessedBlobTx{}, err
	}

	// TODO: remove this check once support for multiple sdk.Msgs in a BlobTx is
	// supported.
	msgs := sdkTx.GetMsgs()
	if len(msgs) != 1 {
		return ProcessedBlobTx{}, ErrMultipleMsgsInBlobTx
	}
	msg := msgs[0]
	pfb, ok := msg.(*MsgPayForBlob)
	if !ok {
		return ProcessedBlobTx{}, ErrNoPFB
	}
	err = pfb.ValidateBasic()
	if err != nil {
		return ProcessedBlobTx{}, err
	}

	blobs, nsIDs, sizes, versions := extractBlobComponents(bTx.Blobs)
	err = ValidatePFBComponents(nsIDs, blobs, versions)
	if err != nil {
		return ProcessedBlobTx{}, err
	}

	// check that the info in the pfb matches that in the blobs
	if !equalUint64Slices(sizes, pfb.BlobSizes) {
		return ProcessedBlobTx{}, ErrBlobSizeMismatch.Wrapf("actual %v declared %v", sizes, pfb.BlobSizes)
	}

	for i := range pfb.NamespaceIds {
		// check that the meta data matches
		if !bytes.Equal(bTx.Blobs[i].NamespaceId, pfb.NamespaceIds[i]) {
			return ProcessedBlobTx{}, ErrNamespaceMismatch.Wrapf("%v %v", bTx.Blobs[i].NamespaceId, pfb.NamespaceIds[i])
		}
	}

	// verify that the commitment of the blob matches that of the PFB
	calculatedCommit, err := CreateMultiShareCommitment(pfb.NamespaceIds, blobs, versions)
	if err != nil {
		return ProcessedBlobTx{}, ErrCalculateCommit
	}
	if !bytes.Equal(calculatedCommit, pfb.ShareCommitment) {
		return ProcessedBlobTx{}, ErrInvalidShareCommit
	}

	protoBlobs := []tmproto.Blob{{NamespaceId: pfb.NamespaceId, Data: blob.Data, ShareVersion: uint32(appconsts.ShareVersionZero)}}

	return ProcessedBlobTx{
		Tx:    bTx.Tx,
		Blobs: bTx.Blobs,
	}, nil
}

func (pBTx ProcessedBlobTx) DataUsed() int {
	// TODO: use something similar to the below when we want multiple blobs per tx
	// used := 0
	// for _, b := range pBTx.Blobs {
	// 	used += len(b.Data)
	// }
	return len(pBTx.Blobs[0].Data)
}

func (pBTx ProcessedBlobTx) SharesUsed() int {
	sharesUsed := 0
	for _, blob := range pBTx.Blobs {
		sharesUsed += shares.BlobSharesUsed(len(blob.Data))
	}
	return sharesUsed
}

func equalUint64Slices(a, b []uint64) bool {
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
