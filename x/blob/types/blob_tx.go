package types

import (
	"bytes"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

// ProcessedBlobTx caches the unmarshalled result of the BlobTx
type ProcessedBlobTx struct {
	Blobs []tmproto.Blob
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
//
// NOTE: ProcessBlobTx does not call the ValidateBasic method on either the
// transaction or messages in that transaction.
func ProcessBlobTx(txcfg client.TxEncodingConfig, bTx tmproto.BlobTx) (ProcessedBlobTx, error) {
	sdkTx, err := txcfg.TxDecoder()(bTx.Tx)
	if err != nil {
		return ProcessedBlobTx{}, err
	}

	msgs := sdkTx.GetMsgs()
	pfbs := make([]*MsgPayForBlob, 0)
	for _, msg := range msgs {
		if sdk.MsgTypeURL(msg) == URLMsgPayForBlob {
			pfb, ok := msg.(*MsgPayForBlob)
			if !ok {
				continue
			}
			pfbs = append(pfbs, pfb)
		}
	}

	if len(pfbs) == 0 {
		return ProcessedBlobTx{}, ErrNoPFBInBlobTx
	}
	if len(pfbs) != len(bTx.Blobs) {
		return ProcessedBlobTx{}, ErrMismatchedNumberOfPFBorBlob
	}

	protoBlobs := make([]tmproto.Blob, len(pfbs))
	for i, pfb := range pfbs {
		err = pfb.ValidateBasic()
		if err != nil {
			return ProcessedBlobTx{}, err
		}

		// todo: modify this to support multiple messages per PFB
		blob := bTx.Blobs[i].Data

		// make sure that the blob size matches the actual size of the blob
		if pfb.BlobSize != uint64(len(blob)) {
			return ProcessedBlobTx{}, ErrDeclaredActualDataSizeMismatch.Wrapf(
				"declared: %d vs actual: %d",
				pfb.BlobSize,
				len(blob),
			)
		}

		// verify that the commitment of the blob matches that of the PFB
		calculatedCommit, err := CreateCommitment(pfb.NamespaceId, blob, appconsts.ShareVersionZero)
		if err != nil {
			return ProcessedBlobTx{}, err // todo: wrap this error with an sdkerror error
		}
		if !bytes.Equal(calculatedCommit, pfb.ShareCommitment) {
			return ProcessedBlobTx{}, ErrInvalidShareCommit
		}

		protoBlobs[i] = tmproto.Blob{NamespaceId: pfb.NamespaceId, Data: blob, ShareVersion: uint32(appconsts.ShareVersionZero)}
	}

	return ProcessedBlobTx{
		Tx:    bTx.Tx,
		Blobs: protoBlobs,
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
