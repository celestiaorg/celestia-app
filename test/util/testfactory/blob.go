package testfactory

import (
	"bytes"
	"encoding/binary"
	"math/rand"

	tmrand "cosmossdk.io/math/unsafe"

	"github.com/celestiaorg/go-square/v2/share"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
)

func GenerateRandomlySizedBlobs(count, maxBlobSize int) []*share.Blob {
	blobs := make([]*share.Blob, count)
	for i := 0; i < count; i++ {
		blobs[i] = GenerateRandomBlob(rand.Intn(maxBlobSize))
		if len(blobs[i].Data()) == 0 {
			i--
		}
	}

	// this is just to let us use assert.Equal
	if count == 0 {
		blobs = nil
	}

	share.SortBlobs(blobs)
	return blobs
}

// GenerateBlobsWithNamespace generates blobs with namespace share.
func GenerateBlobsWithNamespace(count, blobSize int, ns share.Namespace) []*share.Blob {
	blobs := make([]*share.Blob, count)
	for i := 0; i < count; i++ {
		blob, err := share.NewBlob(ns, tmrand.Bytes(blobSize), appconsts.DefaultShareVersion, nil)
		if err != nil {
			panic(err)
		}
		blobs[i] = blob
	}

	// this is just to let us use assert.Equal
	if count == 0 {
		blobs = nil
	}

	return blobs
}

func GenerateRandomBlob(dataSize int) *share.Blob {
	ns := share.MustNewV0Namespace(bytes.Repeat([]byte{0x1}, share.NamespaceVersionZeroIDSize))
	blob, err := share.NewBlob(ns, tmrand.Bytes(dataSize), appconsts.DefaultShareVersion, nil)
	if err != nil {
		panic(err)
	}
	return blob
}

// GenerateRandomBlobOfShareCount returns a blob that spans the given
// number of shares
func GenerateRandomBlobOfShareCount(count int) *share.Blob {
	size := rawBlobSize(share.FirstSparseShareContentSize * count)
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
