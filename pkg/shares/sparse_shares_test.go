package shares

import (
	"bytes"
	"fmt"
	"sort"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	coretypes "github.com/tendermint/tendermint/types"
)

func Test_parseSparseShares(t *testing.T) {
	// exactMsgShareSize is the length of message that will fit exactly into a
	// single share, accounting for namespace id and the length delimiter
	// prepended to each message. Note that the length delimiter can be 1 to 10
	// bytes (varint) but this test assumes it is 2 bytes.
	const exactMsgShareSize = appconsts.SparseShareContentSize - 2

	type test struct {
		name      string
		blobSize  int
		blobCount int
	}

	// each test is ran twice, once using msgSize as an exact size, and again
	// using it as a cap for randomly sized leaves
	tests := []test{
		{"single small msg", appconsts.SparseShareContentSize / 2, 1},
		{"many small msgs", appconsts.SparseShareContentSize / 2, 10},
		{"single big msg", appconsts.SparseShareContentSize * 4, 1},
		{"many big msgs", appconsts.SparseShareContentSize * 4, 10},
		{"single exact size msg", exactMsgShareSize, 1},
		{"many exact size msgs", appconsts.SparseShareContentSize, 10},
	}

	for _, tc := range tests {
		tc := tc
		// run the tests with identically sized blobs
		t.Run(fmt.Sprintf("%s identically sized ", tc.name), func(t *testing.T) {
			blobs := make([]coretypes.Blob, tc.blobCount)
			for i := 0; i < tc.blobCount; i++ {
				blobs[i] = generateRandomBlob(tc.blobSize)
			}

			sort.Sort(coretypes.BlobsByNamespace(blobs))

			shares, _ := SplitMessages(0, nil, blobs, false)
			rawShares := ToBytes(shares)

			parsedMsgs, err := parseSparseShares(rawShares, appconsts.SupportedShareVersions)
			if err != nil {
				t.Error(err)
			}

			// check that the namespaces and data are the same
			for i := 0; i < len(blobs); i++ {
				assert.Equal(t, blobs[i].NamespaceID, parsedMsgs[i].NamespaceID)
				assert.Equal(t, blobs[i].Data, parsedMsgs[i].Data)
			}
		})

		// run the same tests using randomly sized messages with caps of tc.msgSize
		t.Run(fmt.Sprintf("%s randomly sized", tc.name), func(t *testing.T) {
			msgs := generateRandomlySizedBlobs(tc.blobCount, tc.blobSize)
			shares, _ := SplitMessages(0, nil, msgs, false)
			rawShares := make([][]byte, len(shares))
			for i, share := range shares {
				rawShares[i] = []byte(share)
			}

			parsedMsgs, err := parseSparseShares(rawShares, appconsts.SupportedShareVersions)
			if err != nil {
				t.Error(err)
			}

			// check that the namespaces and data are the same
			for i := 0; i < len(msgs); i++ {
				assert.Equal(t, msgs[i].NamespaceID, parsedMsgs[i].NamespaceID)
				assert.Equal(t, msgs[i].Data, parsedMsgs[i].Data)
			}
		})
	}
}

func Test_parseSparseSharesErrors(t *testing.T) {
	type testCase struct {
		name      string
		rawShares [][]byte
	}

	unsupportedShareVersion := 5
	infoByte, _ := NewInfoByte(uint8(unsupportedShareVersion), true)

	rawShare := []byte{}
	rawShare = append(rawShare, namespace.ID{1, 1, 1, 1, 1, 1, 1, 1}...)
	rawShare = append(rawShare, byte(infoByte))
	rawShare = append(rawShare, bytes.Repeat([]byte{0}, appconsts.ShareSize-len(rawShare))...)

	tests := []testCase{
		{
			"share with unsupported share version",
			[][]byte{rawShare},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(*testing.T) {
			_, err := parseSparseShares(tt.rawShares, appconsts.SupportedShareVersions)
			assert.Error(t, err)
		})
	}
}

func TestParsePaddedMsg(t *testing.T) {
	sss := NewSparseShareSplitter()
	randomSmallBlob := generateRandomBlob(appconsts.SparseShareContentSize / 2)
	randomLargeBlob := generateRandomBlob(appconsts.SparseShareContentSize * 4)
	blobs := []coretypes.Blob{
		randomSmallBlob,
		randomLargeBlob,
	}
	sort.Sort(coretypes.BlobsByNamespace(blobs))
	sss.Write(blobs[0])
	sss.WriteNamespacedPaddedShares(4)
	sss.Write(blobs[1])
	sss.WriteNamespacedPaddedShares(10)
	shares := sss.Export()
	rawShares := ToBytes(shares)
	pmsgs, err := parseSparseShares(rawShares, appconsts.SupportedShareVersions)
	require.NoError(t, err)
	require.Equal(t, blobs, pmsgs)
}

func TestSparseShareContainsInfoByte(t *testing.T) {
	message := generateRandomMessageOfShareCount(4)

	sequenceStartInfoByte, err := NewInfoByte(appconsts.ShareVersion, true)
	require.NoError(t, err)

	sequenceContinuationInfoByte, err := NewInfoByte(appconsts.ShareVersion, false)
	require.NoError(t, err)

	type testCase struct {
		name       string
		shareIndex int
		expected   InfoByte
	}
	testCases := []testCase{
		{
			name:       "first share of message",
			shareIndex: 0,
			expected:   sequenceStartInfoByte,
		},
		{
			name:       "second share of message",
			shareIndex: 1,
			expected:   sequenceContinuationInfoByte,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sss := NewSparseShareSplitter()
			sss.Write(message)
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
			blob:     generateRandomMessageOfShareCount(1),
			expected: 1,
		},
		{
			name:     "two shares",
			blob:     generateRandomMessageOfShareCount(2),
			expected: 2,
		},
		{
			name:     "ten shares",
			blob:     generateRandomMessageOfShareCount(10),
			expected: 10,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sss := NewSparseShareSplitter()
			sss.Write(tc.blob)
			got := sss.Count()
			assert.Equal(t, tc.expected, got)
		})
	}
}
