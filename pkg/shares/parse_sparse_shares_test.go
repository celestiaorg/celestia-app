package shares

import (
	"bytes"
	"fmt"
	"sort"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	coretypes "github.com/tendermint/tendermint/types"
)

func Test_parseSparseShares(t *testing.T) {
	type test struct {
		name      string
		blobSize  int
		blobCount int
	}

	// each test is ran twice, once using blobSize as an exact size, and again
	// using it as a cap for randomly sized leaves
	tests := []test{
		{
			name:      "single small blob",
			blobSize:  10,
			blobCount: 1,
		},
		{
			name:      "ten small blobs",
			blobSize:  10,
			blobCount: 10,
		},
		{
			name:      "single big blob",
			blobSize:  appconsts.ContinuationSparseShareContentSize * 4,
			blobCount: 1,
		},
		{
			name:      "many big blobs",
			blobSize:  appconsts.ContinuationSparseShareContentSize * 4,
			blobCount: 10,
		},
		{
			name:      "single exact size blob",
			blobSize:  appconsts.FirstSparseShareContentSize,
			blobCount: 1,
		},
	}

	for _, tc := range tests {
		// run the tests with identically sized blobs
		t.Run(fmt.Sprintf("%s identically sized ", tc.name), func(t *testing.T) {
			blobs := make([]coretypes.Blob, tc.blobCount)
			for i := 0; i < tc.blobCount; i++ {
				blobs[i] = testfactory.GenerateRandomBlob(tc.blobSize)
			}

			sort.Sort(coretypes.BlobsByNamespace(blobs))

			shares, _ := SplitBlobs(0, nil, blobs, false)
			parsedBlobs, err := parseSparseShares(shares, appconsts.SupportedShareVersions)
			if err != nil {
				t.Error(err)
			}

			// check that the namespaces and data are the same
			for i := 0; i < len(blobs); i++ {
				assert.Equal(t, blobs[i].NamespaceID, parsedBlobs[i].NamespaceID, "parsed blob namespace does not match")
				assert.Equal(t, blobs[i].Data, parsedBlobs[i].Data, "parsed blob data does not match")
			}
		})

		// run the same tests using randomly sized blobs with caps of tc.blobSize
		t.Run(fmt.Sprintf("%s randomly sized", tc.name), func(t *testing.T) {
			blobs := testfactory.GenerateRandomlySizedBlobs(tc.blobCount, tc.blobSize)
			shares, _ := SplitBlobs(0, nil, blobs, false)
			parsedBlobs, err := parseSparseShares(shares, appconsts.SupportedShareVersions)
			if err != nil {
				t.Error(err)
			}

			// check that the namespaces and data are the same
			for i := 0; i < len(blobs); i++ {
				assert.Equal(t, blobs[i].NamespaceID, parsedBlobs[i].NamespaceID)
				assert.Equal(t, blobs[i].Data, parsedBlobs[i].Data)
			}
		})
	}
}

func Test_parseSparseSharesErrors(t *testing.T) {
	type testCase struct {
		name   string
		shares []Share
	}

	unsupportedShareVersion := 5
	infoByte, _ := NewInfoByte(uint8(unsupportedShareVersion), true)

	rawShare := []byte{}
	rawShare = append(rawShare, namespace.ID{1, 1, 1, 1, 1, 1, 1, 1}...)
	rawShare = append(rawShare, byte(infoByte))
	rawShare = append(rawShare, bytes.Repeat([]byte{0}, appconsts.ShareSize-len(rawShare))...)
	share, err := NewShare(rawShare)
	if err != nil {
		t.Fatal(err)
	}

	tests := []testCase{
		{
			"share with unsupported share version",
			[]Share{*share},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(*testing.T) {
			_, err := parseSparseShares(tt.shares, appconsts.SupportedShareVersions)
			assert.Error(t, err)
		})
	}
}

func Test_parseSparseSharesWithNamespacedPadding(t *testing.T) {
	sss := NewSparseShareSplitter()
	randomSmallBlob := testfactory.GenerateRandomBlob(appconsts.ContinuationSparseShareContentSize / 2)
	randomLargeBlob := testfactory.GenerateRandomBlob(appconsts.ContinuationSparseShareContentSize * 4)
	blobs := []coretypes.Blob{
		randomSmallBlob,
		randomLargeBlob,
	}
	sort.Sort(coretypes.BlobsByNamespace(blobs))

	err := sss.Write(blobs[0])
	require.NoError(t, err)

	err = sss.WriteNamespacedPaddedShares(4)
	require.NoError(t, err)

	err = sss.Write(blobs[1])
	require.NoError(t, err)

	err = sss.WriteNamespacedPaddedShares(10)
	require.NoError(t, err)

	shares := sss.Export()
	pblobs, err := parseSparseShares(shares, appconsts.SupportedShareVersions)
	require.NoError(t, err)
	require.Equal(t, blobs, pblobs)
}
