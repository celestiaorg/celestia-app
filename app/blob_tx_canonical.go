package app

import (
	"bytes"

	blobtx "github.com/celestiaorg/go-square/v4/tx"
)

// blobTxIsCanonical reports whether raw is the canonical protobuf encoding of
// btx. The canonical encoding of a blob tx is defined as the output of
// go-square's MarshalBlobTx for its decoded content: every field encoded
// exactly once, in ascending field number order, with no unknown fields.
//
// UnmarshalBlobTx accepts non-canonical encodings (unknown fields, or repeated
// singular fields where the last value wins) and silently drops the extra
// bytes, so re-marshaling the decoded blob tx yields the canonical form. Raw
// bytes that differ carry padding that never reaches the block, yet would
// inflate the mempool, gossip, and the proposal byte budget without being paid
// for. Requiring canonical encoding closes that gap.
//
// This predicate is consensus critical: it is enforced in CheckTx,
// PrepareProposal (separateTxs), and ProcessProposal.
func blobTxIsCanonical(raw []byte, btx *blobtx.BlobTx) bool {
	canonical, err := blobtx.MarshalBlobTx(btx.Tx, btx.Blobs...)
	if err != nil {
		return false
	}
	return bytes.Equal(raw, canonical)
}
