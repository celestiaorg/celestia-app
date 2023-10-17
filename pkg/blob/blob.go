package blob

import (
	"bytes"
	"errors"
	fmt "fmt"
	math "math"
	"sort"

	"github.com/celestiaorg/celestia-app/pkg/namespace"
)

// ProtoBlobTxTypeID is included in each encoded BlobTx to help prevent
// decoding binaries that are not actually BlobTxs.
const ProtoBlobTxTypeID = "BLOB"

// NewBlob creates a new coretypes.Blob from the provided data after performing
// basic stateless checks over it.
func New(ns namespace.Namespace, blob []byte, shareVersion uint8) *Blob {
	return &Blob{
		NamespaceId:      ns.ID,
		Data:             blob,
		ShareVersion:     uint32(shareVersion),
		NamespaceVersion: uint32(ns.Version),
	}
}

// Namespace returns the namespace of the blob
func (b Blob) Namespace() namespace.Namespace {
	return namespace.Namespace{
		Version: uint8(b.NamespaceVersion),
		ID:      b.NamespaceId,
	}
}

// Validate runs a stateless validity check on the form of the struct.
func (b *Blob) Validate() error {
	if b == nil {
		return errors.New("nil blob")
	}
	if len(b.NamespaceId) != namespace.NamespaceIDSize {
		return fmt.Errorf("namespace id must be %d bytes", namespace.NamespaceIDSize)
	}
	if b.ShareVersion > math.MaxUint8 {
		return errors.New("share version can not be greater than MaxShareVersion")
	}
	if b.NamespaceVersion > namespace.NamespaceVersionMax {
		return errors.New("namespace version can not be greater than MaxNamespaceVersion")
	}
	if len(b.Data) == 0 {
		return errors.New("blob data can not be empty")
	}
	return nil
}

// UnmarshalBlobTx attempts to unmarshal a transaction into blob transaction. If an
// error is thrown, false is returned.
func UnmarshalBlobTx(tx []byte) (bTx BlobTx, isBlob bool) {
	err := bTx.Unmarshal(tx)
	if err != nil {
		return BlobTx{}, false
	}
	// perform some quick basic checks to prevent false positives
	if bTx.TypeId != ProtoBlobTxTypeID {
		return bTx, false
	}
	if len(bTx.Blobs) == 0 {
		return bTx, false
	}
	for _, b := range bTx.Blobs {
		if len(b.NamespaceId) != namespace.NamespaceIDSize {
			return bTx, false
		}
	}
	return bTx, true
}

// MarshalBlobTx creates a BlobTx using a normal transaction and some number of
// blobs.
//
// NOTE: Any checks on the blobs or the transaction must be performed in the
// application
func MarshalBlobTx(tx []byte, blobs ...*Blob) ([]byte, error) {
	bTx := BlobTx{
		Tx:     tx,
		Blobs:  blobs,
		TypeId: ProtoBlobTxTypeID,
	}
	return bTx.Marshal()
}

func Sort(blobs []*Blob) {
	sort.SliceStable(blobs, func(i, j int) bool {
		return bytes.Compare(blobs[i].Namespace().Bytes(), blobs[j].Namespace().Bytes()) < 0
	})
}
