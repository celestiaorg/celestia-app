package testfactory

import (
	"bytes"
	"sort"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/namespace"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

func Repeat[T any](s T, count int) []T {
	ss := make([]T, count)
	for i := 0; i < count; i++ {
		ss[i] = s
	}
	return ss
}

// GenerateRandNamespacedRawData returns random data of length count. Each chunk
// of random data is of size shareSize and is prefixed with a random blob
// namespace.
func GenerateRandNamespacedRawData(count int) (result [][]byte) {
	for i := 0; i < count; i++ {
		rawData := tmrand.Bytes(appconsts.ShareSize)
		namespace := namespace.RandomBlobNamespace().Bytes()
		copy(rawData, namespace)
		result = append(result, rawData)
	}

	sortByteArrays(result)
	return result
}

func sortByteArrays(src [][]byte) {
	sort.Slice(src, func(i, j int) bool { return bytes.Compare(src[i], src[j]) < 0 })
}
