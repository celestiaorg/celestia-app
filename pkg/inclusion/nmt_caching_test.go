package inclusion

import (
	"bytes"
	"sort"
	"testing"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/pkg/da"
	"github.com/celestiaorg/celestia-app/v3/pkg/wrapper"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/celestiaorg/nmt"
	"github.com/celestiaorg/rsmt2d"
	tmrand "github.com/cometbft/cometbft/libs/rand"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWalkCachedSubTreeRoot(t *testing.T) {
	// create the first main tree
	strc := newSubTreeRootCacher()
	squareSize := uint64(8)
	tr := wrapper.NewErasuredNamespacedMerkleTree(squareSize, 0, nmt.NodeVisitor(strc.Visit))
	ns1 := share.MustNewV0Namespace(bytes.Repeat([]byte{1}, share.NamespaceVersionZeroIDSize))

	data := append(ns1.Bytes(), []byte("data")...)
	for i := 0; i < 8; i++ {
		err := tr.Push(data)
		assert.NoError(t, err)
	}
	highestRoot, err := tr.Root()
	assert.NoError(t, err)

	// create a short sub tree
	shortSubTree := wrapper.NewErasuredNamespacedMerkleTree(squareSize, 0)
	for i := 0; i < 2; i++ {
		err := shortSubTree.Push(data)
		assert.NoError(t, err)
	}
	shortSTR, err := shortSubTree.Root()
	assert.NoError(t, err)

	// create a tall sub tree root
	tallSubTree := wrapper.NewErasuredNamespacedMerkleTree(squareSize, 0)
	for i := 0; i < 4; i++ {
		err := tallSubTree.Push(data)
		assert.NoError(t, err)
	}
	tallSTR, err := tallSubTree.Root()
	assert.NoError(t, err)

	type test struct {
		name          string
		path          []WalkInstruction
		subTreeRoot   []byte
		expectedError string
	}

	tests := []test{
		{
			"left most short sub tree",
			[]WalkInstruction{WalkLeft, WalkLeft},
			shortSTR,
			"",
		},
		{
			"left middle short sub tree",
			[]WalkInstruction{WalkLeft, WalkRight},
			shortSTR,
			"",
		},
		{
			"right middle short sub tree",
			[]WalkInstruction{WalkRight, WalkLeft},
			shortSTR,
			"",
		},
		{
			"right most short sub tree",
			[]WalkInstruction{WalkRight, WalkRight},
			shortSTR,
			"",
		},
		{
			"left most tall sub tree",
			[]WalkInstruction{WalkLeft},
			tallSTR,
			"",
		},
		{
			"right most tall sub tree",
			[]WalkInstruction{WalkRight},
			tallSTR,
			"",
		},
		{
			"right most tall sub tree",
			[]WalkInstruction{WalkRight, WalkRight, WalkRight, WalkRight},
			tallSTR,
			"did not find sub tree root",
		},
	}

	for _, tt := range tests {
		foundSubRoot, err := strc.walk(highestRoot, tt.path)
		if tt.expectedError != "" {
			require.Error(t, err, tt.name)
			assert.Contains(t, err.Error(), tt.expectedError, tt.name)
			continue
		}

		require.NoError(t, err)
		require.Equal(t, tt.subTreeRoot, foundSubRoot, tt.name)
	}
}

func TestEDSSubRootCacher(t *testing.T) {
	squareSize := 8
	d := generateRandNamespacedRawData(squareSize * squareSize)
	stc := NewSubtreeCacher(uint64(squareSize))

	eds, err := rsmt2d.ComputeExtendedDataSquare(d, appconsts.DefaultCodec(), stc.Constructor)
	require.NoError(t, err)

	dah, err := da.NewDataAvailabilityHeader(eds)
	require.NoError(t, err)

	for i := range dah.RowRoots[:squareSize] {
		expectedSubTreeRoots := calculateSubTreeRoots(t, eds.Row(uint(i))[:squareSize], 2)
		require.NotNil(t, expectedSubTreeRoots)
		// note: the depth is one greater than expected because we're dividing
		// the row in half when we calculate the expected roots.
		result, err := stc.getSubTreeRoot(dah, i, []WalkInstruction{false, false, false})
		require.NoError(t, err)
		assert.Equal(t, expectedSubTreeRoots[0], result)
	}
}

// calculateSubTreeRoots generates the subtree roots for a given row. If the
// selected depth is too deep for the tree, nil is returned. It relies on
// passing a row whose length is a power of 2 and assumes that the row is
// **NOT** extended since calculating subtree root for erasure data using the
// nmt wrapper makes this difficult.
func calculateSubTreeRoots(t *testing.T, row [][]byte, depth int) [][]byte {
	subLeafRange := len(row)
	for i := 0; i < depth; i++ {
		subLeafRange /= 2
	}

	if subLeafRange == 0 || subLeafRange%2 != 0 {
		return nil
	}

	count := len(row) / subLeafRange
	subTreeRoots := make([][]byte, count)
	chunks := chunkSlice(row, subLeafRange)
	for i, rowChunk := range chunks {
		tr := wrapper.NewErasuredNamespacedMerkleTree(uint64(len(row)), 0)
		for _, r := range rowChunk {
			err := tr.Push(r)
			assert.NoError(t, err)
		}
		root, err := tr.Root()
		assert.NoError(t, err)
		subTreeRoots[i] = root
	}

	return subTreeRoots
}

func chunkSlice(slice [][]byte, chunkSize int) [][][]byte {
	var chunks [][][]byte
	for i := 0; i < len(slice); i += chunkSize {
		end := i + chunkSize

		// necessary check to avoid slicing beyond
		// slice capacity
		if end > len(slice) {
			end = len(slice)
		}

		chunks = append(chunks, slice[i:end])
	}

	return chunks
}

// generateRandNamespacedRawData returns random data of length count. Each chunk
// of random data is of size shareSize and is prefixed with a random blob
// namespace.
func generateRandNamespacedRawData(count int) (result [][]byte) {
	for i := 0; i < count; i++ {
		rawData := tmrand.Bytes(share.ShareSize)
		namespace := share.RandomBlobNamespace().Bytes()
		copy(rawData, namespace)
		result = append(result, rawData)
	}

	sortByteArrays(result)
	return result
}

func sortByteArrays(src [][]byte) {
	sort.Slice(src, func(i, j int) bool { return bytes.Compare(src[i], src[j]) < 0 })
}
