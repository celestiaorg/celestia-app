package shares

import (
	"bytes"

	tmrand "github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/types"
)

func GenerateRandomlySizedBlobs(count, maxBlobSize int) []*Blob {
	blobs := make([]*Blob, count)
	for i := 0; i < count; i++ {
		blobs[i] = GenerateRandomBlob(tmrand.Intn(maxBlobSize))
		if len(blobs[i].Data) == 0 {
			i--
		}
	}

	// this is just to let us use assert.Equal
	if count == 0 {
		blobs = nil
	}

	SortBlobs(blobs)
	return blobs
}

// GenerateBlobsWithNamespace generates blobs with namespace ns.
func GenerateBlobsWithNamespace(count int, blobSize int, ns Namespace) []types.Blob {
	blobs := make([]types.Blob, count)
	for i := 0; i < count; i++ {
		blobs[i] = types.Blob{
			NamespaceVersion: ns.Version,
			NamespaceID:      ns.ID,
			Data:             tmrand.Bytes(blobSize),
			ShareVersion:     ShareVersionZero,
		}
	}

	// this is just to let us use assert.Equal
	if count == 0 {
		blobs = nil
	}

	return blobs
}

func GenerateRandomBlob(dataSize int) *Blob {
	ns := MustNewV0Namespace(bytes.Repeat([]byte{0x1}, NamespaceVersionZeroIDSize))
	return NewBlob(ns, tmrand.Bytes(dataSize), ShareVersionZero)
}

// GenerateRandomBlobOfShareCount returns a blob that spans the given
// number of shares
func GenerateRandomBlobOfShareCount(count int) *Blob {
	size := rawBlobSize(FirstSparseShareContentSize * count)
	return GenerateRandomBlob(size)
}

// rawBlobSize returns the raw blob size that can be used to construct a
// blob of totalSize bytes. This function is useful in tests to account for
// the delimiter length that is prefixed to a blob's data.
func rawBlobSize(totalSize int) int {
	return totalSize - DelimLen(uint64(totalSize))
}
