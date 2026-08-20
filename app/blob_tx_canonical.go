package app

import (
	"bytes"

	blobtx "github.com/celestiaorg/go-square/v4/tx"
)

// blobTxIsCanonical reports whether raw is the canonical protobuf encoding of
// btx, i.e. re-marshaling the decoded blob tx reproduces raw. UnmarshalBlobTx
// accepts non-canonical encodings (unknown or repeated fields used as padding)
// and drops the extra bytes; requiring canonical encoding rejects that padding.
// Enforced in CheckTx, PrepareProposal, and ProcessProposal.
func blobTxIsCanonical(raw []byte, btx *blobtx.BlobTx) bool {
	canonical, err := blobtx.MarshalBlobTx(btx.Tx, btx.Blobs...)
	if err != nil {
		return false
	}
	return bytes.Equal(raw, canonical)
}
