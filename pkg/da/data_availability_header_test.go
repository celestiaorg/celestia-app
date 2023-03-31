package da

import (
	"bytes"
	"sort"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNilDataAvailabilityHeaderHashDoesntCrash(t *testing.T) {
	// This follows RFC-6962, i.e. `echo -n '' | sha256sum`
	emptyBytes := []byte{
		0xe3, 0xb0, 0xc4, 0x42, 0x98, 0xfc, 0x1c, 0x14, 0x9a, 0xfb, 0xf4, 0xc8,
		0x99, 0x6f, 0xb9, 0x24, 0x27, 0xae, 0x41, 0xe4, 0x64, 0x9b, 0x93, 0x4c, 0xa4, 0x95, 0x99, 0x1b,
		0x78, 0x52, 0xb8, 0x55,
	}

	assert.Equal(t, emptyBytes, (*DataAvailabilityHeader)(nil).Hash())
	assert.Equal(t, emptyBytes, new(DataAvailabilityHeader).Hash())
}

func TestMinDataAvailabilityHeader(t *testing.T) {
	dah := MinDataAvailabilityHeader()
	expectedHash := []byte{0xad, 0x23, 0x6a, 0x2e, 0x4c, 0x5f, 0xca, 0x6c, 0xdb, 0xae, 0x5d, 0x5e, 0xdf, 0x79, 0xe8, 0x8e, 0x84, 0xc5, 0x2e, 0xed, 0x62, 0xeb, 0xd0, 0xb6, 0x5d, 0x18, 0xb2, 0x7c, 0x32, 0xa8, 0xbc, 0x58}
	require.Equal(t, expectedHash, dah.hash)
	require.NoError(t, dah.ValidateBasic())
}

func TestNewDataAvailabilityHeader(t *testing.T) {
	type test struct {
		name         string
		expectedHash []byte
		squareSize   uint64
		shares       [][]byte
	}

	tests := []test{
		{
			name:         "typical",
			expectedHash: []byte{0xeb, 0xfd, 0xb5, 0xc5, 0x52, 0x59, 0xd6, 0xe, 0x72, 0x6b, 0xde, 0x58, 0x7, 0x9a, 0x58, 0xd2, 0x18, 0x7b, 0xc9, 0x44, 0x7, 0x6e, 0xbe, 0x74, 0x47, 0x67, 0x45, 0xa3, 0xb7, 0x3a, 0x52, 0x47},
			squareSize:   2,
			shares:       generateShares(4),
		},
		{
			name:         "max square size",
			expectedHash: []byte{0x48, 0x28, 0xa9, 0xef, 0x79, 0xc2, 0x12, 0x12, 0xc, 0x53, 0x83, 0x27, 0x55, 0x7d, 0x42, 0xdd, 0x64, 0x74, 0xad, 0x4e, 0x82, 0xcb, 0xa0, 0x43, 0xed, 0x14, 0x2, 0x54, 0x0, 0x3b, 0xf6, 0x11},
			squareSize:   appconsts.DefaultMaxSquareSize,
			shares:       generateShares(appconsts.DefaultMaxSquareSize * appconsts.DefaultMaxSquareSize),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eds, err := ExtendShares(tt.squareSize, tt.shares)
			require.NoError(t, err)
			resdah := NewDataAvailabilityHeader(eds)
			require.Equal(t, tt.squareSize*2, uint64(len(resdah.ColumnRoots)), tt.name)
			require.Equal(t, tt.squareSize*2, uint64(len(resdah.RowsRoots)), tt.name)
			require.Equal(t, tt.expectedHash, resdah.hash, tt.name)
		})
	}
}

func TestExtendShares(t *testing.T) {
	type test struct {
		name        string
		expectedErr bool
		squareSize  uint64
		shares      [][]byte
	}

	tests := []test{
		{
			name:        "too large square size",
			expectedErr: true,
			squareSize:  appconsts.DefaultMaxSquareSize + 1,
			shares:      generateShares((appconsts.DefaultMaxSquareSize + 1) * (appconsts.DefaultMaxSquareSize + 1)),
		},
		{
			name:        "invalid number of shares",
			expectedErr: true,
			squareSize:  2,
			shares:      generateShares(5),
		},
	}

	for _, tt := range tests {
		tt := tt
		eds, err := ExtendShares(tt.squareSize, tt.shares)
		if tt.expectedErr {
			require.NotNil(t, err)
			continue
		}
		require.NoError(t, err)
		require.Equal(t, tt.squareSize*2, eds.Width(), tt.name)
	}
}

func TestDataAvailabilityHeaderProtoConversion(t *testing.T) {
	type test struct {
		name string
		dah  DataAvailabilityHeader
	}

	shares := generateShares(appconsts.DefaultMaxSquareSize * appconsts.DefaultMaxSquareSize)
	eds, err := ExtendShares(appconsts.DefaultMaxSquareSize, shares)
	require.NoError(t, err)
	bigdah := NewDataAvailabilityHeader(eds)

	tests := []test{
		{
			name: "min",
			dah:  MinDataAvailabilityHeader(),
		},
		{
			name: "max",
			dah:  bigdah,
		},
	}

	for _, tt := range tests {
		tt := tt
		pdah, err := tt.dah.ToProto()
		require.NoError(t, err)
		resDah, err := DataAvailabilityHeaderFromProto(pdah)
		require.NoError(t, err)
		resDah.Hash() // calc the hash to make the comparisons fair
		require.Equal(t, tt.dah, *resDah, tt.name)
	}
}

func Test_DAHValidateBasic(t *testing.T) {
	type test struct {
		name      string
		dah       DataAvailabilityHeader
		expectErr bool
		errStr    string
	}

	shares := generateShares(appconsts.DefaultMaxSquareSize * appconsts.DefaultMaxSquareSize)
	eds, err := ExtendShares(appconsts.DefaultMaxSquareSize, shares)
	require.NoError(t, err)
	bigdah := NewDataAvailabilityHeader(eds)

	// make a mutant dah that has too many roots
	var tooBigDah DataAvailabilityHeader
	tooBigDah.ColumnRoots = make([][]byte, appconsts.DefaultMaxSquareSize*appconsts.DefaultMaxSquareSize)
	tooBigDah.RowsRoots = make([][]byte, appconsts.DefaultMaxSquareSize*appconsts.DefaultMaxSquareSize)
	copy(tooBigDah.ColumnRoots, bigdah.ColumnRoots)
	copy(tooBigDah.RowsRoots, bigdah.RowsRoots)
	tooBigDah.ColumnRoots = append(tooBigDah.ColumnRoots, bytes.Repeat([]byte{1}, 32))
	tooBigDah.RowsRoots = append(tooBigDah.RowsRoots, bytes.Repeat([]byte{1}, 32))
	// make a mutant dah that has too few roots
	var tooSmallDah DataAvailabilityHeader
	tooSmallDah.ColumnRoots = [][]byte{bytes.Repeat([]byte{2}, 32)}
	tooSmallDah.RowsRoots = [][]byte{bytes.Repeat([]byte{2}, 32)}
	// use a bad hash
	badHashDah := MinDataAvailabilityHeader()
	badHashDah.hash = []byte{1, 2, 3, 4}
	// dah with not equal number of roots
	mismatchDah := MinDataAvailabilityHeader()
	mismatchDah.ColumnRoots = append(mismatchDah.ColumnRoots, bytes.Repeat([]byte{2}, 32))

	tests := []test{
		{
			name: "min",
			dah:  MinDataAvailabilityHeader(),
		},
		{
			name: "max",
			dah:  bigdah,
		},
		{
			name:      "too big dah",
			dah:       tooBigDah,
			expectErr: true,
			errStr:    "maximum valid DataAvailabilityHeader has at most",
		},
		{
			name:      "too small dah",
			dah:       tooSmallDah,
			expectErr: true,
			errStr:    "minimum valid DataAvailabilityHeader has at least",
		},
		{
			name:      "bash hash",
			dah:       badHashDah,
			expectErr: true,
			errStr:    "wrong hash",
		},
		{
			name:      "mismatched roots",
			dah:       mismatchDah,
			expectErr: true,
			errStr:    "unequal number of row and column roots",
		},
	}

	for _, tt := range tests {
		tt := tt
		err := tt.dah.ValidateBasic()
		if tt.expectErr {
			require.True(t, strings.Contains(err.Error(), tt.errStr), tt.name)
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)
	}
}

// generateShares generates count number of shares with a constant namespace and
// share contents.
func generateShares(count int) (shares [][]byte) {
	ns1 := appns.MustNewV0(bytes.Repeat([]byte{1}, appns.NamespaceVersionZeroIDSize))

	for i := 0; i < count; i++ {
		share := generateShare(ns1.Bytes())
		shares = append(shares, share)
	}
	sortByteArrays(shares)
	return shares
}

func generateShare(namespace []byte) (share []byte) {
	remainder := bytes.Repeat([]byte{0xFF}, appconsts.ShareSize-len(namespace))
	share = append(share, namespace...)
	share = append(share, remainder...)
	return share
}

func sortByteArrays(arr [][]byte) {
	sort.Slice(arr, func(i, j int) bool {
		return bytes.Compare(arr[i], arr[j]) < 0
	})
}
