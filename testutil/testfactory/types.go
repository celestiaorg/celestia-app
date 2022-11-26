package testfactory

import (
	mrand "math/rand"
	"sort"

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

func GenerateRandomBlob(size int) types.Blob {
	blob := types.Blob{
		NamespaceID:  tmrand.Bytes(appconsts.NamespaceSize),
		Data:         tmrand.Bytes(size),
		ShareVersion: appconsts.ShareVersionZero,
	}
	return blob
}

func GenerateRandomlySizedTransactions(count, maxSize int) types.Txs {
	txs := make(types.Txs, count)
	for i := 0; i < count; i++ {
		size := mrand.Intn(maxSize)
		if size == 0 {
			size = 1
		}
		txs[i] = GenerateRandomTransaction(1, size)[0]
	}
	return txs
}

func GenerateRandomTransaction(count, size int) types.Txs {
	txs := make(types.Txs, count)
	for i := 0; i < count; i++ {
		tx := make([]byte, size)
		_, err := mrand.Read(tx)
		if err != nil {
			panic(err)
		}
		txs[i] = tx
	}
	return txs
}
