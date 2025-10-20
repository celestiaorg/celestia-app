package da

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	appconstsv5 "github.com/celestiaorg/celestia-app/v6/pkg/appconsts/v5"
	"github.com/celestiaorg/celestia-app/v6/pkg/wrapper"
	sharev2 "github.com/celestiaorg/go-square/v2/share"
	sh "github.com/celestiaorg/go-square/v3/share"
	"github.com/celestiaorg/rsmt2d"
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

// TestMinDataAvailabilityHeader tests the minimum valid data availability header.
//
// This test verifies that MinDataAvailabilityHeader() produces a deterministic hash
// that matches the expected value. The expected hash is generated through the following process:
//
// 1. Create minimum shares: MinShareCount (1) tail padding shares are created
// 2. Extend shares: The single share is extended using Reed-Solomon encoding to create a 2x2 extended data square
// 3. Extract roots: Row and column merkle roots are computed from the extended square:
//   - 2 row roots (one for each row of the extended square)
//   - 2 column roots (one for each column of the extended square)
//
// 4. Compute hash: A binary merkle tree is built from the concatenated row and column roots
// (rowRoots || columnRoots) to produce the final data availability header hash
//
// The expectedHash below (0x3d96b7d2...) represents the merkle root of the concatenated
// row and column roots from a 2x2 extended data square containing one tail padding share.
// This hash is deterministic and will always be the same for the minimum data availability header
// since it represents the smallest possible valid data square in the Celestia network.
func TestMinDataAvailabilityHeader(t *testing.T) {
	dah := MinDataAvailabilityHeader()
	// Expected hash generated from merkle root of (rowRoots || columnRoots)
	// where the roots come from a 2x2 extended data square with one tail padding share
	expectedHash := []byte{0x3d, 0x96, 0xb7, 0xd2, 0x38, 0xe7, 0xe0, 0x45, 0x6f, 0x6a, 0xf8, 0xe7, 0xcd, 0xf0, 0xa6, 0x7b, 0xd6, 0xcf, 0x9c, 0x20, 0x89, 0xec, 0xb5, 0x59, 0xc6, 0x59, 0xdc, 0xaa, 0x1f, 0x88, 0x3, 0x53}
	require.Equal(t, expectedHash, dah.hash)
	require.NoError(t, dah.ValidateBasic())
}

type (
	extendFunc    = func([][]byte) (*rsmt2d.ExtendedDataSquare, error)
	constructFunc = func(txs [][]byte, appVersion uint64, maxSquareSize int) (*rsmt2d.ExtendedDataSquare, error)
)

// extendSharesWithPool works exactly the same as ExtendShares,
// but it uses treePool to reuse the allocs.
func extendSharesWithPool(s [][]byte) (*rsmt2d.ExtendedDataSquare, error) {
	treePool, err := wrapper.DefaultPreallocatedTreePool(512)
	if err != nil {
		return nil, err
	}
	return ExtendSharesWithTreePool(s, treePool)
}

// constructEDSWithPool works exactly the same as ConstructEDS,
// but it uses treePool to reuse the allocs.
func constructEDSWithPool(txs [][]byte, appVersion uint64, maxSquareSize int) (*rsmt2d.ExtendedDataSquare, error) {
	treePool, err := wrapper.DefaultPreallocatedTreePool(512)
	if err != nil {
		return nil, err
	}
	return ConstructEDSWithTreePool(txs, appVersion, maxSquareSize, treePool)
}

func TestMinDataAvailabilityHeaderBackwardsCompatibility(t *testing.T) {
	for _, extendShares := range []extendFunc{
		extendSharesWithPool,
		ExtendShares,
	} {
		dahv3 := MinDataAvailabilityHeader()
		shareV2 := sharev2.ToBytes(sharev2.TailPaddingShares(appconsts.MinShareCount))
		eds, err := extendShares(shareV2)
		require.NoError(t, err)
		dahV2, err := NewDataAvailabilityHeader(eds)
		require.NoError(t, err)
		require.Equal(t, dahv3.hash, dahV2.hash)
	}
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
			expectedHash: []byte{0x8f, 0x7d, 0xd2, 0xfc, 0x5f, 0x70, 0xd9, 0xc3, 0xca, 0xc7, 0x7e, 0x81, 0x5e, 0x0, 0x9d, 0x69, 0x4c, 0xd6, 0xd2, 0x49, 0xe7, 0x62, 0xdb, 0xbb, 0xb1, 0x99, 0xb7, 0x17, 0x7d, 0x6a, 0xd3, 0x88},
			squareSize:   uint64(appconsts.SquareSizeUpperBound),
			shares:       generateShares(appconsts.SquareSizeUpperBound * appconsts.SquareSizeUpperBound),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, extendShares := range []extendFunc{
				extendSharesWithPool,
				ExtendShares,
			} {
				eds, err := extendShares(tt.shares)
				require.NoError(t, err)
				got, err := NewDataAvailabilityHeader(eds)
				require.NoError(t, err)
				require.Equal(t, tt.squareSize*2, uint64(len(got.ColumnRoots)))
				require.Equal(t, tt.squareSize*2, uint64(len(got.RowRoots)))
				require.Equal(t, tt.expectedHash, got.hash)
			}
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
			shares:      generateShares((appconsts.SquareSizeUpperBound + 1) * (appconsts.SquareSizeUpperBound + 1)),
		},
		{
			name:        "invalid number of shares",
			expectedErr: true,
			shares:      generateShares(5),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, extendShares := range []extendFunc{
				extendSharesWithPool,
				ExtendShares,
			} {
				_, err := extendShares(tt.shares)
				if tt.expectedErr {
					require.NotNil(t, err)
				} else {
					require.NoError(t, err)
				}
			}
		})
	}
}

func TestDataAvailabilityHeaderProtoConversion(t *testing.T) {
	for _, extendShares := range []extendFunc{
		extendSharesWithPool,
		ExtendShares,
	} {
		testDataAvailabilityHeaderProtoConversion(t, extendShares)
	}
}

func testDataAvailabilityHeaderProtoConversion(t *testing.T, extendShares func([][]byte) (*rsmt2d.ExtendedDataSquare, error)) {
	type test struct {
		name string
		dah  DataAvailabilityHeader
	}

	shares := generateShares(appconsts.SquareSizeUpperBound * appconsts.SquareSizeUpperBound)
	eds, err := extendShares(shares)
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
	for _, extendShares := range []extendFunc{
		extendSharesWithPool,
		ExtendShares,
	} {
		testDAHValidateBasic(t, extendShares)
	}
}

func testDAHValidateBasic(t *testing.T, extendShares func([][]byte) (*rsmt2d.ExtendedDataSquare, error)) {
	type test struct {
		name      string
		dah       DataAvailabilityHeader
		expectErr bool
		errStr    string
	}

	maxSize := appconsts.SquareSizeUpperBound * appconsts.SquareSizeUpperBound

	shares := generateShares(maxSize)
	eds, err := extendShares(shares)
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
			name:      "bad hash",
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
			want: appconsts.SquareSizeUpperBound,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.dah.SquareSize()
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestConstructEDS_Versions(t *testing.T) {
	minAppVersion := uint64(0)
	maxAppVersion := appconsts.Version + 1 // even future versions won't error and assume compatibility with v3
	for appVersion := minAppVersion; appVersion <= maxAppVersion; appVersion++ {
		t.Run(fmt.Sprintf("app version %d", appVersion), func(t *testing.T) {
			for _, constructEDS := range []constructFunc{
				constructEDSWithPool,
				ConstructEDS,
			} {
				shares := generateShares(4)
				maxSquareSize := -1
				eds, err := constructEDS(shares, appVersion, maxSquareSize)
				if appVersion == 0 {
					require.Error(t, err)
					require.Nil(t, eds)
				} else {
					require.NoError(t, err)
					require.NotNil(t, eds)
				}
			}
		})
	}
}

func TestConstructEDS_SquareSize(t *testing.T) {
	type testCase struct {
		name         string
		appVersion   uint64
		maxSquare    int
		expectedSize int
	}
	testCases := []testCase{
		{
			name:         "v5 version with custom square size",
			appVersion:   appconstsv5.Version,
			maxSquare:    4,
			expectedSize: 4,
		},
		{
			name:         "v5 version with default square size",
			appVersion:   appconstsv5.Version,
			maxSquare:    -1,
			expectedSize: appconstsv5.SquareSizeUpperBound,
		},
		{
			name:         "latest version with custom square size",
			appVersion:   appconsts.Version,
			maxSquare:    8,
			expectedSize: 8,
		},
		{
			name:         "latest version with default square size",
			appVersion:   appconsts.Version,
			maxSquare:    -1,
			expectedSize: appconsts.SquareSizeUpperBound,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, construct := range []constructFunc{
				constructEDSWithPool,
				ConstructEDS,
			} {
				txLength := sh.AvailableBytesFromCompactShares((tc.expectedSize * tc.expectedSize) - 1)
				tx := bytes.Repeat([]byte{0x1}, txLength)
				eds, err := construct([][]byte{tx}, tc.appVersion, tc.maxSquare)
				require.NoError(t, err)
				require.NotNil(t, eds)
				// The EDS width should be 2*expectedSize
				require.Equal(t, tc.expectedSize*2, int(eds.Width()))
			}
		})
	}
}

// generateShares generates count number of shares with a constant namespace and
// share contents.
func generateShares(count int) (shares [][]byte) {
	ns1 := sh.MustNewV0Namespace(bytes.Repeat([]byte{1}, sh.NamespaceVersionZeroIDSize))

	for range count {
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
	return maxDataAvailabilityHeaderWithExtendShares(t, ExtendShares)
}

// maxDataAvailabilityHeaderWithExtendShares returns a DataAvailabilityHeader the maximum square
// size using the provided extendShares function. This should only be used for testing.
func maxDataAvailabilityHeaderWithExtendShares(t *testing.T, extendShares func([][]byte) (*rsmt2d.ExtendedDataSquare, error)) (dah DataAvailabilityHeader) {
	shares := generateShares(appconsts.SquareSizeUpperBound * appconsts.SquareSizeUpperBound)

	eds, err := extendShares(shares)
	require.NoError(t, err)

	dah, err = NewDataAvailabilityHeader(eds)
	require.NoError(t, err)

	return dah
}
