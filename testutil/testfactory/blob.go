package testfactory

import (
	"sort"

	nmtnamespace "github.com/celestiaorg/nmt/namespace"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
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

	sort.Sort(types.BlobsByNamespace(blobs))
	return blobs
}

// GenerateBlobsWithNamespace generates blobs with namespace ID `nID`.
func GenerateBlobsWithNamespace(count, blobSize int, nID nmtnamespace.ID) types.BlobsByNamespace {
	blobs := make([]types.Blob, count)
	for i := 0; i < count; i++ {
		blobs[i] = types.Blob{
			NamespaceID: nID,
			Data:        tmrand.Bytes(blobSize),
		}
	}

	// this is just to let us use assert.Equal
	if count == 0 {
		blobs = nil
	}

	return blobs
}

func GenerateRandomBlob(size int) types.Blob {
	blob := types.Blob{
		NamespaceID:  tmrand.Bytes(appconsts.NamespaceSize),
		Data:         tmrand.Bytes(size),
		ShareVersion: appconsts.ShareVersionZero,
	}
	return blob
}
