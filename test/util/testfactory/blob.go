package testfactory

import (
	"bytes"
	"encoding/binary"
	"sort"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/types"
)

func GenerateRandomlySizedBlobs(count, maxBlobSize int) []types.Blob {
	blobs := make([]types.Blob, count)
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

	sort.Slice(blobs, func(i, j int) bool { return bytes.Compare(blobs[i].NamespaceID, blobs[j].NamespaceID) < 0 })
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

func GenerateRandomBlob(dataSize int) types.Blob {
	blob := types.Blob{
		NamespaceVersion: appns.NamespaceVersionZero,
		NamespaceID:      append(appns.NamespaceVersionZeroPrefix, bytes.Repeat([]byte{0x1}, appns.NamespaceVersionZeroIDSize)...),
		Data:             tmrand.Bytes(dataSize),
		ShareVersion:     appconsts.ShareVersionZero,
	}
	return blob
}

// GenerateRandomBlobOfShareCount returns a blob that spans the given
// number of shares
func GenerateRandomBlobOfShareCount(count int) types.Blob {
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

// BlobsByNamespace implements sort.Interface for Blob
type BlobsByNamespace []types.Blob

func (b BlobsByNamespace) Len() int {
	return len(b)
}

func (b BlobsByNamespace) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}

func (b BlobsByNamespace) Less(i, j int) bool {
	// The following comparison is `<` and not `<=` because bytes.Compare returns 0 for if a == b.
	// We want this comparison to return `false` if a == b because:
	// If both Less(i, j) and Less(j, i) are false,
	// then the elements at index i and j are considered equal.
	// See https://pkg.go.dev/sort#Interface
	return bytes.Compare(b[i].NamespaceID, b[j].NamespaceID) < 0
}
