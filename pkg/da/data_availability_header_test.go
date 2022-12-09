package da

import (
	"bytes"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
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
	expectedHash := []byte{
		0x6f, 0x52, 0xda, 0xc1, 0x65, 0x45, 0xe4, 0x57, 0x25, 0xbe, 0x6e, 0xa3, 0x2a, 0xed, 0x55, 0x26,
		0x6e, 0x45, 0x3, 0x48, 0x0, 0xee, 0xe1, 0xd8, 0x7c, 0x94, 0x28, 0xf4, 0x84, 0x4e, 0xa4, 0x7a,
	}
	require.Equal(t, expectedHash, dah.hash)
	require.NoError(t, dah.ValidateBasic())
	// important note: also see the types.TestEmptyBlockDataAvailabilityHeader test
	// which ensures that empty block data results in the minimum data availability
	// header
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
			name: "typical",
			expectedHash: []byte{
				0x57, 0x71, 0xc6, 0x77, 0x2f, 0x32, 0x95, 0x73, 0xaa, 0xb8, 0x20, 0xd1, 0xbe, 0x4c, 0xc2, 0x21,
				0x7d, 0x54, 0xb6, 0x7e, 0xf2, 0x4f, 0xbc, 0xd3, 0x9a, 0x95, 0x15, 0xd0, 0x92, 0x63, 0xc1, 0xf9,
			},
			squareSize: 2,
			shares:     generateShares(4, 1),
		},
		{
			name: "max square size",
			expectedHash: []byte{
				0xbf, 0xe5, 0x8f, 0x4b, 0xae, 0x2b, 0x65, 0x8b, 0xa8, 0xcb, 0xf9, 0xee, 0x8c, 0x6a, 0x1f, 0x72,
				0xa9, 0x58, 0xc4, 0xcc, 0xca, 0x41, 0x4c, 0xbf, 0x8b, 0x18, 0xf9, 0x53, 0xe, 0xb1, 0x40, 0x54,
			},
			squareSize: appconsts.DefaultMaxSquareSize,
			shares:     generateShares(appconsts.DefaultMaxSquareSize*appconsts.DefaultMaxSquareSize, 99),
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
			shares:      generateShares((appconsts.DefaultMaxSquareSize+1)*(appconsts.DefaultMaxSquareSize+1), 1),
		},
		{
			name:        "invalid number of shares",
			expectedErr: true,
			squareSize:  2,
			shares:      generateShares(5, 1),
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

	shares := generateShares(appconsts.DefaultMaxSquareSize*appconsts.DefaultMaxSquareSize, 1)
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

	shares := generateShares(appconsts.DefaultMaxSquareSize*appconsts.DefaultMaxSquareSize, 1)
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

func generateShares(count int, repeatByte byte) [][]byte {
	shares := make([][]byte, count)
	for i := 0; i < count; i++ {
		shares[i] = bytes.Repeat([]byte{repeatByte}, appconsts.ShareSize)
	}
	return shares
}
