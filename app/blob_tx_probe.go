package app

import (
	"bytes"

	blobtx "github.com/celestiaorg/go-square/v4/tx"
)

// A BlobTx with type_id BLOB necessarily contains those four literal bytes,
// even if another field uses a noncanonical wire encoding. Most SDK txs do
// not, so this avoids a full protobuf decode for the common non-blob case.
func unmarshalBlobTxIfPresent(rawTx []byte) (*blobtx.BlobTx, bool, error) {
	if !bytes.Contains(rawTx, []byte(blobtx.ProtoBlobTxTypeID)) {
		return nil, false, nil
	}
	return blobtx.UnmarshalBlobTx(rawTx)
}
