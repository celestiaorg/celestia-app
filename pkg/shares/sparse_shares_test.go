package shares

import (
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/testutil/testfactory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	coretypes "github.com/tendermint/tendermint/types"
)

func TestSparseShareContainsInfoByte(t *testing.T) {
	blob := generateRandomBlobOfShareCount(4)

	sequenceStartInfoByte, err := NewInfoByte(appconsts.ShareVersionZero, true)
	require.NoError(t, err)

	sequenceContinuationInfoByte, err := NewInfoByte(appconsts.ShareVersionZero, false)
	require.NoError(t, err)

	type testCase struct {
		name       string
		shareIndex int
		expected   InfoByte
	}
	testCases := []testCase{
		{
			name:       "first share of blob",
			shareIndex: 0,
			expected:   sequenceStartInfoByte,
		},
		{
			name:       "second share of blob",
			shareIndex: 1,
			expected:   sequenceContinuationInfoByte,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sss := NewSparseShareSplitter()
			err := sss.Write(blob)
			assert.NoError(t, err)
			shares := sss.Export()
			got, err := shares[tc.shareIndex].InfoByte()
			require.NoError(t, err)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestSparseShareSplitterCount(t *testing.T) {
	type testCase struct {
		name     string
		blob     coretypes.Blob
		expected int
	}
	testCases := []testCase{
		{
			name:     "one share",
			blob:     generateRandomBlobOfShareCount(1),
			expected: 1,
		},
		{
			name:     "two shares",
			blob:     generateRandomBlobOfShareCount(2),
			expected: 2,
		},
		{
			name:     "ten shares",
			blob:     generateRandomBlobOfShareCount(10),
			expected: 10,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sss := NewSparseShareSplitter()
			err := sss.Write(tc.blob)
			assert.NoError(t, err)
			got := sss.Count()
			assert.Equal(t, tc.expected, got)
		})
	}
}

// generateRandomBlobOfShareCount returns a blob that spans the given
// number of shares
func generateRandomBlobOfShareCount(count int) coretypes.Blob {
	size := rawBlobSize(appconsts.FirstSparseShareContentSize * count)
	return testfactory.GenerateRandomBlob(size)
}

// rawBlobSize returns the raw blob size that can be used to construct a
// blob of totalSize bytes. This function is useful in tests to account for
// the delimiter length that is prefixed to a blob's data.
func rawBlobSize(totalSize int) int {
	return totalSize - DelimLen(uint64(totalSize))
}
