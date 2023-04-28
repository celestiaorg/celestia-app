package wrapper

import (
	"bytes"
	"crypto/sha256"
	"sort"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/namespace"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/nmt"
	"github.com/celestiaorg/rsmt2d"
	"github.com/stretchr/testify/assert"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

func TestPushErasuredNamespacedMerkleTree(t *testing.T) {
	testCases := []struct {
		name       string
		squareSize int
	}{
		{
			name:       "squareSize = 8, extendedSquareSize = 16",
			squareSize: 8,
		},
		{
			name:       "squareSize = 128, extendedSquareSize = 256",
			squareSize: 128,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tree := NewErasuredNamespacedMerkleTree(uint64(tc.squareSize), 0)

			for _, d := range generateErasuredData(t, tc.squareSize, appconsts.DefaultCodec()) {
				// push test data to the tree. push will panic if there's an
				// error.
				tree.Push(d)
			}
		})
	}
}

// TestRootErasuredNamespacedMerkleTree checks that the root of an erasured NMT
// is different from the root of a standard NMT. The roots should be different
// because the erasured NMT should use the parity namespace ID for leaves pushed
// to the second half of the tree.
func TestRootErasuredNamespacedMerkleTree(t *testing.T) {
	size := 8
	data := generateRandNamespacedRawData(size)
	tree := NewErasuredNamespacedMerkleTree(uint64(size), 0)
	nmtTree := nmt.New(sha256.New(), nmt.NamespaceIDSize(namespace.NamespaceSize), nmt.IgnoreMaxNamespace(true))

	for _, d := range data {
		tree.Push(d)
		err := nmtTree.Push(d)
		if err != nil {
			t.Error(err)
		}
	}

	root, err := nmtTree.Root()
	assert.NoError(t, err)
	assert.NotEqual(t, root, tree.Root())
}

func TestErasureNamespacedMerkleTreePanics(t *testing.T) {
	testCases := []struct {
		name  string
		pFunc assert.PanicTestFunc
	}{
		{
			"push over square size",
			assert.PanicTestFunc(
				func() {
					data := generateErasuredData(t, 16, appconsts.DefaultCodec())
					tree := NewErasuredNamespacedMerkleTree(uint64(15), 0)
					for _, d := range data {
						tree.Push(d)
					}
				}),
		},
		{
			"push in incorrect lexigraphic order",
			assert.PanicTestFunc(
				func() {
					data := generateErasuredData(t, 16, appconsts.DefaultCodec())
					tree := NewErasuredNamespacedMerkleTree(uint64(16), 0)
					for i := len(data) - 1; i > 0; i-- {
						tree.Push(data[i])
					}
				},
			),
		},
		{
			"push data that is too short to contain a namespace ID",
			assert.PanicTestFunc(
				func() {
					data := []byte{0x1}
					tree := NewErasuredNamespacedMerkleTree(uint64(16), 0)
					tree.Push(data)
				},
			),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Panics(t, tc.pFunc, tc.name)
		})
	}
}

func TestComputeExtendedDataSquare(t *testing.T) {
	squareSize := 4
	// data for a 4X4 square
	data := generateRandNamespacedRawData(squareSize * squareSize)

	_, err := rsmt2d.ComputeExtendedDataSquare(data, appconsts.DefaultCodec(), NewConstructor(uint64(squareSize)))
	assert.NoError(t, err)
}

// generateErasuredData generates random data and then erasure codes it. It
// returns a slice that is twice as long as numLeaves because it returns the
// original data + erasured data.
func generateErasuredData(t *testing.T, numLeaves int, codec rsmt2d.Codec) [][]byte {
	raw := generateRandNamespacedRawData(numLeaves)
	erasuredData, err := codec.Encode(raw)
	if err != nil {
		t.Error(err)
	}
	return append(raw, erasuredData...)
}

// generateRandNamespacedRawData returns random data of length count. Each chunk
// of random data is of size shareSize and is prefixed with a random blob
// namespace.
func generateRandNamespacedRawData(count int) (result [][]byte) {
	for i := 0; i < count; i++ {
		rawData := tmrand.Bytes(appconsts.ShareSize)
		namespace := appns.RandomBlobNamespace().Bytes()
		copy(rawData, namespace)
		result = append(result, rawData)
	}

	sortByteArrays(result)
	return result
}

func sortByteArrays(src [][]byte) {
	sort.Slice(src, func(i, j int) bool { return bytes.Compare(src[i], src[j]) < 0 })
}
