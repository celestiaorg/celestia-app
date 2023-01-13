package shares

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/testutil/testfactory"
	"github.com/celestiaorg/rsmt2d"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	coretypes "github.com/tendermint/tendermint/types"
)

func TestMerge(t *testing.T) {
	type test struct {
		name      string
		txCount   int
		blobCount int
		maxSize   int // max size of each tx or blob
	}

	tests := []test{
		{"one of each random small size", 1, 1, 40},
		{"one of each random large size", 1, 1, 400},
		{"many of each random large size", 10, 10, 40},
		{"many of each random large size", 10, 10, 400},
		{"only transactions", 10, 0, 400},
		{"only blobs", 0, 10, 400},
	}

	for _, tc := range tests {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			// generate random data
			data := generateRandomBlockData(
				tc.txCount,
				tc.blobCount,
				tc.maxSize,
			)

			shares, err := Split(data, false)
			require.NoError(t, err)
			rawShares := ToBytes(shares)

			eds, err := rsmt2d.ComputeExtendedDataSquare(rawShares, appconsts.DefaultCodec(), rsmt2d.NewDefaultTree)
			if err != nil {
				t.Error(err)
			}

			res, err := merge(eds)
			if err != nil {
				t.Fatal(err)
			}

			res.SquareSize = data.SquareSize

			assert.Equal(t, data, res)
		})
	}
}

// TestPadFirstIndexedBlob ensures that we are adding padding to the first share
// instead of calculating the value.
func TestPadFirstIndexedBlob(t *testing.T) {
	tx := tmrand.Bytes(300)
	blob := tmrand.Bytes(300)
	index := 100
	indexedTx, err := coretypes.MarshalIndexWrapper(tx, 100)
	require.NoError(t, err)

	bd := coretypes.Data{
		Txs: []coretypes.Tx{indexedTx},
		Blobs: []coretypes.Blob{
			{NamespaceID: []byte{1, 2, 3, 4, 5, 6, 7, 8}, Data: blob, ShareVersion: appconsts.ShareVersionZero},
		},
		SquareSize: 64,
	}

	shares, err := Split(bd, true)
	require.NoError(t, err)

	resShare, err := shares[index].RawData()
	require.NoError(t, err)

	require.True(t, bytes.Contains(resShare, blob))
}

func TestFuzz_Merge(t *testing.T) {
	t.Skip()
	// run random shares through processCompactShares for a minute
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			TestMerge(t)
		}
	}
}

// generateRandomBlockData returns randomly generated block data for testing purposes
func generateRandomBlockData(txCount, blobCount, maxSize int) (data coretypes.Data) {
	data.Txs = testfactory.GenerateRandomlySizedTxs(txCount, maxSize)
	data.Blobs = testfactory.GenerateRandomlySizedBlobs(blobCount, maxSize)
	data.SquareSize = appconsts.DefaultMaxSquareSize
	return data
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

func TestSequenceLen(t *testing.T) {
	type testCase struct {
		name    string
		share   Share
		wantLen uint32
		wantErr bool
	}
	firstShare := []byte{
		1, 1, 1, 1, 1, 1, 1, 1, // namespace
		1,           // info byte
		0, 0, 0, 10, // sequence len
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10, // data
	}
	firstShareWithLongSequence := []byte{
		1, 1, 1, 1, 1, 1, 1, 1, // namespace
		1,           // info byte
		0, 0, 1, 67, // sequence len
	}
	continuationShare := []byte{
		1, 1, 1, 1, 1, 1, 1, 1, // namespace
		0, // info byte
	}
	compactShare := []byte{
		0, 0, 0, 0, 0, 0, 0, 1, // namespace
		1,           // info byte
		0, 0, 0, 10, // sequence len
	}
	noInfoByte := []byte{
		0, 0, 0, 0, 0, 0, 0, 1, // namespace
	}
	noSequenceLen := []byte{
		0, 0, 0, 0, 0, 0, 0, 1, // namespace
		1, // info byte
	}
	testCases := []testCase{
		{
			name:    "first share",
			share:   firstShare,
			wantLen: 10,
			wantErr: false,
		},
		{
			name:    "first share with long sequence",
			share:   firstShareWithLongSequence,
			wantLen: 323,
			wantErr: false,
		},
		{
			name:    "continuation share",
			share:   continuationShare,
			wantLen: 0,
			wantErr: false,
		},
		{
			name:    "compact share",
			share:   compactShare,
			wantLen: 10,
			wantErr: false,
		},
		{
			name:    "no info byte returns error",
			share:   noInfoByte,
			wantLen: 0,
			wantErr: true,
		},
		{
			name:    "no sequence len returns error",
			share:   noSequenceLen,
			wantLen: 0,
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			len, err := tc.share.SequenceLen()

			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			if tc.wantLen != len {
				t.Errorf("want %d, got %d", tc.wantLen, len)
			}
		})
	}
}

func TestRawData(t *testing.T) {
	type testCase struct {
		name    string
		share   Share
		want    []byte
		wantErr bool
	}
	firstSparseShare := []byte{
		1, 1, 1, 1, 1, 1, 1, 1, // namespace
		1,           // info byte
		0, 0, 0, 10, // sequence len
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10, // data
	}
	continuationSparseShare := []byte{
		1, 1, 1, 1, 1, 1, 1, 1, // namespace
		0,                             // info byte
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10, // data
	}
	firstCompactShare := []byte{
		0, 0, 0, 0, 0, 0, 0, 1, // namespace
		1,           // info byte
		0, 0, 0, 10, // sequence len
		0, 0, 0, 15, // reserved bytes
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10, // data
	}
	continuationCompactShare := []byte{
		0, 0, 0, 0, 0, 0, 0, 1, // namespace
		0,          // info byte
		0, 0, 0, 0, // reserved bytes
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10, // data
	}
	noSequenceLen := []byte{
		0, 0, 0, 0, 0, 0, 0, 1, // namespace
		1, // info byte
	}
	notEnoughSequenceLenBytes := []byte{
		0, 0, 0, 0, 0, 0, 0, 1, // namespace
		1,        // info byte
		0, 0, 10, // sequence len
	}
	testCases := []testCase{
		{
			name:  "first sparse share",
			share: firstSparseShare,
			want:  []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		},
		{
			name:  "continuation sparse share",
			share: continuationSparseShare,
			want:  []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		},
		{
			name:  "first compact share",
			share: firstCompactShare,
			want:  []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		},
		{
			name:  "continuation compact share",
			share: continuationCompactShare,
			want:  []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		},
		{
			name:    "no sequence len returns error",
			share:   noSequenceLen,
			wantErr: true,
		},
		{
			name:    "not enough sequence len bytes returns error",
			share:   notEnoughSequenceLenBytes,
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rawData, err := tc.share.RawData()
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.Equal(t, tc.want, rawData)
		})
	}
}
