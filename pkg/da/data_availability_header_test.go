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
	expectedHash := []byte{0xe9, 0x5, 0x28, 0x49, 0xf, 0x1d, 0x51, 0x67, 0x29, 0x2c, 0x1f, 0x1b, 0x83, 0xe1, 0x74, 0x2a, 0x27, 0x48, 0x17, 0x34, 0x12, 0xc9, 0x1d, 0xf7, 0xdd, 0x1, 0x96, 0x78, 0xa4, 0x62, 0xb9, 0x77}
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
			expectedHash: []byte{0x5b, 0x27, 0x3e, 0x3a, 0x5d, 0x9e, 0x90, 0x25, 0x58, 0x21, 0xb7, 0xe0, 0x4d, 0x4b, 0xaa, 0xde, 0x37, 0xa6, 0x6f, 0xcc, 0xd, 0x16, 0x6f, 0x9e, 0xe0, 0x7f, 0xbe, 0x8, 0xb4, 0x41, 0xc8, 0xa6},
			squareSize:   2,
			shares:       generateShares(4),
		},
		{
			name:         "max square size",
			expectedHash: []byte{0xce, 0x5c, 0xf3, 0xc9, 0x15, 0xeb, 0xbf, 0xb0, 0x67, 0xe1, 0xa5, 0x97, 0x35, 0xf3, 0x25, 0x7b, 0x1c, 0x47, 0x74, 0x1f, 0xec, 0x6a, 0x33, 0x19, 0x7f, 0x8f, 0xc2, 0x4a, 0xe, 0xe2, 0xbe, 0x73},
			squareSize:   appconsts.MaxSquareSize,
			shares:       generateShares(appconsts.MaxSquareSize * appconsts.MaxSquareSize),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eds, err := ExtendShares(tt.shares)
			require.NoError(t, err)
			resdah := NewDataAvailabilityHeader(eds)
			require.Equal(t, tt.squareSize*2, uint64(len(resdah.ColumnRoots)), tt.name)
			require.Equal(t, tt.squareSize*2, uint64(len(resdah.RowRoots)), tt.name)
			require.Equal(t, tt.expectedHash, resdah.hash, tt.name)
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
			shares:      generateShares((appconsts.MaxSquareSize + 1) * (appconsts.MaxSquareSize + 1)),
		},
		{
			name:        "invalid number of shares",
			expectedErr: true,
			shares:      generateShares(5),
		},
	}

	for _, tt := range tests {
		tt := tt
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

	shares := generateShares(appconsts.MaxSquareSize * appconsts.MaxSquareSize)
	eds, err := ExtendShares(shares)
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

	shares := generateShares(appconsts.MaxSquareSize * appconsts.MaxSquareSize)
	eds, err := ExtendShares(shares)
	require.NoError(t, err)
	bigdah := NewDataAvailabilityHeader(eds)

	// make a mutant dah that has too many roots
	var tooBigDah DataAvailabilityHeader
	tooBigDah.ColumnRoots = make([][]byte, appconsts.MaxSquareSize*appconsts.MaxSquareSize)
	tooBigDah.RowRoots = make([][]byte, appconsts.MaxSquareSize*appconsts.MaxSquareSize)
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
