package da

import (
	"bytes"
	"sort"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	sh "github.com/celestiaorg/go-square/v2/share"
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
	expectedHash := []byte{0x3d, 0x96, 0xb7, 0xd2, 0x38, 0xe7, 0xe0, 0x45, 0x6f, 0x6a, 0xf8, 0xe7, 0xcd, 0xf0, 0xa6, 0x7b, 0xd6, 0xcf, 0x9c, 0x20, 0x89, 0xec, 0xb5, 0x59, 0xc6, 0x59, 0xdc, 0xaa, 0x1f, 0x88, 0x3, 0x53}
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
			expectedHash: []byte{0xb5, 0x6e, 0x4d, 0x25, 0x1a, 0xc2, 0x66, 0xf4, 0xb9, 0x1c, 0xc5, 0x46, 0x4b, 0x3f, 0xc7, 0xef, 0xcb, 0xdc, 0x88, 0x80, 0x64, 0x64, 0x74, 0x96, 0xd1, 0x31, 0x33, 0xf0, 0xdc, 0x65, 0xac, 0x25},
			squareSize:   2,
			shares:       generateShares(2 * 2),
		},
		{
			name:         "max square size",
			expectedHash: []byte{0xb, 0xd3, 0xab, 0xee, 0xac, 0xfb, 0xb0, 0xb9, 0x2d, 0xfb, 0xda, 0xc4, 0xa1, 0x54, 0x86, 0x8e, 0x3c, 0x4e, 0x79, 0x66, 0x6f, 0x7f, 0xcf, 0x6c, 0x62, 0xb, 0xb9, 0xd, 0xd3, 0xa0, 0xdc, 0xf0},
			squareSize:   uint64(appconsts.DefaultSquareSizeUpperBound),
			shares:       generateShares(appconsts.DefaultSquareSizeUpperBound * appconsts.DefaultSquareSizeUpperBound),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eds, err := ExtendShares(tt.shares)
			require.NoError(t, err)
			got, err := NewDataAvailabilityHeader(eds)
			require.NoError(t, err)
			require.Equal(t, tt.squareSize*2, uint64(len(got.ColumnRoots)), tt.name)
			require.Equal(t, tt.squareSize*2, uint64(len(got.RowRoots)), tt.name)
			require.Equal(t, tt.expectedHash, got.hash, tt.name)
		})
	}
}

func TestExtendShares(t *testing.T) {
	type test struct {
		name        string
		expectedErr bool
		shares      [][]byte
	}

	tests := []test{
		{
			name:        "too large square size",
			expectedErr: true,
			shares:      generateShares((appconsts.DefaultSquareSizeUpperBound + 1) * (appconsts.DefaultSquareSizeUpperBound + 1)),
		},
		{
			name:        "invalid number of shares",
			expectedErr: true,
			shares:      generateShares(5),
		},
	}

	for _, tt := range tests {
		_, err := ExtendShares(tt.shares)
		if tt.expectedErr {
			require.NotNil(t, err)
			continue
		}
		require.NoError(t, err)
	}
}

func TestDataAvailabilityHeaderProtoConversion(t *testing.T) {
	type test struct {
		name string
		dah  DataAvailabilityHeader
	}

	shares := generateShares(appconsts.DefaultSquareSizeUpperBound * appconsts.DefaultSquareSizeUpperBound)
	eds, err := ExtendShares(shares)
	require.NoError(t, err)
	bigdah, err := NewDataAvailabilityHeader(eds)
	require.NoError(t, err)

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

	maxSize := appconsts.DefaultSquareSizeUpperBound * appconsts.DefaultSquareSizeUpperBound

	shares := generateShares(maxSize)
	eds, err := ExtendShares(shares)
	require.NoError(t, err)
	bigdah, err := NewDataAvailabilityHeader(eds)
	require.NoError(t, err)

	// make a mutant dah that has too many roots
	var tooBigDah DataAvailabilityHeader
	tooBigDah.ColumnRoots = make([][]byte, maxSize)
	tooBigDah.RowRoots = make([][]byte, maxSize)
	copy(tooBigDah.ColumnRoots, bigdah.ColumnRoots)
	copy(tooBigDah.RowRoots, bigdah.RowRoots)
	tooBigDah.ColumnRoots = append(tooBigDah.ColumnRoots, bytes.Repeat([]byte{1}, 32))
	tooBigDah.RowRoots = append(tooBigDah.RowRoots, bytes.Repeat([]byte{1}, 32))
	// make a mutant dah that has too few roots
	var tooSmallDah DataAvailabilityHeader
	tooSmallDah.ColumnRoots = [][]byte{bytes.Repeat([]byte{2}, 32)}
	tooSmallDah.RowRoots = [][]byte{bytes.Repeat([]byte{2}, 32)}
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
		err := tt.dah.ValidateBasic()
		if tt.expectErr {
			require.True(t, strings.Contains(err.Error(), tt.errStr), tt.name)
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)
	}
}

func TestSquareSize(t *testing.T) {
	type testCase struct {
		name string
		dah  DataAvailabilityHeader
		want int
	}

	testCases := []testCase{
		{
			name: "min data availability header has an original square size of 1",
			dah:  MinDataAvailabilityHeader(),
			want: 1,
		},
		{
			name: "max data availability header has an original square size of default square size upper bound",
			dah:  maxDataAvailabilityHeader(t),
			want: appconsts.DefaultSquareSizeUpperBound,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.dah.SquareSize()
			assert.Equal(t, tc.want, got)
		})
	}
}

// generateShares generates count number of shares with a constant namespace and
// share contents.
func generateShares(count int) (shares [][]byte) {
	ns1 := sh.MustNewV0Namespace(bytes.Repeat([]byte{1}, sh.NamespaceVersionZeroIDSize))

	for i := 0; i < count; i++ {
		share := generateShare(ns1.Bytes())
		shares = append(shares, share)
	}
	sortByteArrays(shares)
	return shares
}

func generateShare(namespace []byte) (share []byte) {
	remainder := bytes.Repeat([]byte{0xFF}, sh.ShareSize-len(namespace))
	share = append(share, namespace...)
	share = append(share, remainder...)
	return share
}

func sortByteArrays(arr [][]byte) {
	sort.Slice(arr, func(i, j int) bool {
		return bytes.Compare(arr[i], arr[j]) < 0
	})
}

// maxDataAvailabilityHeader returns a DataAvailabilityHeader the maximum square
// size. This should only be used for testing.
func maxDataAvailabilityHeader(t *testing.T) (dah DataAvailabilityHeader) {
	shares := generateShares(appconsts.DefaultSquareSizeUpperBound * appconsts.DefaultSquareSizeUpperBound)

	eds, err := ExtendShares(shares)
	require.NoError(t, err)

	dah, err = NewDataAvailabilityHeader(eds)
	require.NoError(t, err)

	return dah
}
