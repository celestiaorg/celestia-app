package testfactory

import (
	"bytes"
	"encoding/binary"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/go-square/blob"
	appns "github.com/celestiaorg/go-square/namespace"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/types"
)

func GenerateRandomlySizedBlobs(count, maxBlobSize int) []*blob.Blob {
	blobs := make([]*blob.Blob, count)
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

	blob.Sort(blobs)
	return blobs
}

// GenerateBlobsWithNamespace generates blobs with namespace ns.
func GenerateBlobsWithNamespace(count int, blobSize int, ns appns.Namespace) []types.Blob {
	blobs := make([]types.Blob, count)
	for i := 0; i < count; i++ {
		blobs[i] = types.Blob{
			NamespaceVersion: ns.Version,
			NamespaceID:      ns.ID,
			Data:             tmrand.Bytes(blobSize),
			ShareVersion:     appconsts.ShareVersionZero,
		}
	}

	// this is just to let us use assert.Equal
	if count == 0 {
		blobs = nil
	}

	return blobs
}

func GenerateRandomBlob(dataSize int) *blob.Blob {
	ns := appns.MustNewV0(bytes.Repeat([]byte{0x1}, appns.NamespaceVersionZeroIDSize))
	return blob.New(ns, tmrand.Bytes(dataSize), appconsts.ShareVersionZero)
}

// GenerateRandomBlobOfShareCount returns a blob that spans the given
// number of shares
func GenerateRandomBlobOfShareCount(count int) *blob.Blob {
	size := rawBlobSize(appconsts.FirstSparseShareContentSize * count)
	return GenerateRandomBlob(size)
}

// rawBlobSize returns the raw blob size that can be used to construct a
// blob of totalSize bytes. This function is useful in tests to account for
// the delimiter length that is prefixed to a blob's data.
func rawBlobSize(totalSize int) int {
	return totalSize - DelimLen(uint64(totalSize))
}

// DelimLen calculates the length of the delimiter for a given unit size
func DelimLen(size uint64) int {
	lenBuf := make([]byte, binary.MaxVarintLen64)
	return binary.PutUvarint(lenBuf, size)
}
