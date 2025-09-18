package da

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	appconstsv5 "github.com/celestiaorg/celestia-app/v6/pkg/appconsts/v5"
	sharev2 "github.com/celestiaorg/go-square/v2/share"
	sh "github.com/celestiaorg/go-square/v3/share"
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

func TestMinDataAvailabilityHeaderVersioning(t *testing.T) {
	dah := MinDataAvailabilityHeader()
	shareV2 := sharev2.ToBytes(sharev2.TailPaddingShares(appconsts.MinShareCount))
	eds, err := ExtendShares(shareV2)
	require.NoError(t, err)
	dahV2, err := NewDataAvailabilityHeader(eds)
	require.NoError(t, err)
	require.Equal(t, dah.hash, dahV2.hash)
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
			shares:      generateShares((appconsts.SquareSizeUpperBound + 1) * (appconsts.SquareSizeUpperBound + 1)),
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

	shares := generateShares(appconsts.SquareSizeUpperBound * appconsts.SquareSizeUpperBound)
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

	maxSize := appconsts.SquareSizeUpperBound * appconsts.SquareSizeUpperBound

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
	maxAppVersion := appconsts.Version + 1
	for appVersion := minAppVersion; appVersion <= maxAppVersion; appVersion++ {
		t.Run(fmt.Sprintf("app version %d", appVersion), func(t *testing.T) {
			shares := generateShares(4)
			maxSquareSize := -1
			eds, err := ConstructEDS(shares, appVersion, maxSquareSize)
			if appVersion > appconsts.Version || appVersion == 0 {
				require.Error(t, err)
				require.Nil(t, eds)
			} else {
				require.NoError(t, err)
				require.NotNil(t, eds)
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
			txLength := sh.AvailableBytesFromCompactShares((tc.expectedSize * tc.expectedSize) - 1)
			tx := bytes.Repeat([]byte{0x1}, txLength)
			eds, err := ConstructEDS([][]byte{tx}, tc.appVersion, tc.maxSquare)
			require.NoError(t, err)
			require.NotNil(t, eds)
			// The EDS width should be 2*expectedSize
			require.Equal(t, tc.expectedSize*2, int(eds.Width()))
		})
	}
}

func TestConstructEDS_historicalBlocks(t *testing.T) {
	t.Run("v1", func(t *testing.T) {
		appVersion := uint64(1)
		maxSquareSize := -1

		// Transaction data from Celestia mainnet block height 2371493
		// Data fetched via curl --location 'https://docs-demo.celestia-mainnet.quiknode.pro/block?height=2371493'
		txs := [][]byte{
			mustDecodeBase64(t, "CpMBCpABCi4vY29zbW9zLmRpc3RyaWJ1dGlvbi52MWJldGExLk1zZ1dpdGhkcmF3RGVsZWdhdG9yUmV3YXJkEl4KLWNlbGVzdGlhMXQ4djN0bmV2cHg2cHV0bjh3dmYwa3k2d3NrNm1oc2ptNGo3Znd3Ei1jZWxlc3RpYXZhbG9wZXIxdDh2M3R0ZW52cHg2cHV0bjh3dmYwa3k2d3NrNm1oc2ptNGo3Znd3EmoKWgpQCigvY29zbW9zLmNyeXB0by5zZWNwMjU2azEuUHViS2V5EiQKIgIhLdEOPjebYLnBdBaQLTAZYcYVis+bNvzRprCbVDygA0X4EgQKAggBGAQSBhDjzwYQrwEaQMJRkLaRyEuWJsQJd8WK+MlHQqxgUNaQAAs+TYJ5D4j+Uw3aQPXsJ1JI9E4CRNCnikmvMuEDXMVTSjMl+UqBRamX"),
			mustDecodeBase64(t, "CqUCCqICCi8vaWJjLmFwcGxpY2F0aW9ucy50cmFuc2Zlci52MS5Nc2dUcmFuc2ZlchJvCgh0cmFuc2ZlchISY29ubmVjdGlvbi0wGgV1dGlhIgoKMTAwMDAwMDAwMCotY2VsZXN0aWExcDlleTZ0ZTV0MHR3OTVkY3VzNnJhZ210Y3dmNjg2M2t1dTY4NjI6CgoFdXRpYRIBMRJqClgKUAooL2Nvc21vcy5jcnlwdG8uc2VjcDI1NmsxLlB1YktleRIkCiIDD1n0QXSgkIDUwNWUslZvX4YRFT4M5kOJalhhRDnMPdfvEgQKAggBGAMSBhDjzwYQqAEaQGhWitHPy0+WjL+EW119B8FAELg+E39ksX/8pNIQRBfUQhP//Yvwl8FlGjWK0BGdLtAreohj+A1d3Nc="),
			mustDecodeBase64(t, "CqUCCqICCi8vaWJjLmFwcGxpY2F0aW9ucy50cmFuc2Zlci52MS5Nc2dUcmFuc2ZlchJvCgh0cmFuc2ZlchISY29ubmVjdGlvbi0wGgV1dGlhIgoKMTAwMDAwMDAwMCotY2VsZXN0aWExZnN5NTdoejZnMjA3NWNtd3RmaGtqdTRkNzRsMGw3ejl3M2YwcTI6CgoFdXRpYRIBMRJqClgKUAooL2Nvc21vcy5jcnlwdG8uc2VjcDI1NmsxLlB1YktleRIkCiIDD1n0QXSgkIDUwNWUslZvX4YRFT4M5kOJalhhRDnMPdfvEgQKAggBGAISBhDjzwYQqAEaQGhWitHPy0+WjL+EW119B8FAELg+E39ksX/8pNIQRBfUQhP//Yvwl8FlGjWK0BGdLtAreohj+A1d3Nc="),
			mustDecodeBase64(t, "CtgECtUECiplaWJjLmNvcmUuY2xpZW50LnYxLk1zZ1VwZGF0ZUNsaWVudBKKBAowNy10ZW5kZXJtaW50LTAShAQKKi9pYmMubGlnaHRjbGllbnRzLnRlbmRlcm1pbnQudjEuSGVhZGVyElYIDBIECAkQABgBIAIqSAogKOHbhOKD5MYvXa1MBgfJQTTCFNXhPfwHfXAIwGLUYKgSJNGfxzGb3L3wEr4Gqm7L3V4+cjZHgZYwQVNEgWDQdKPFGTIqSAog6Z/HMZvcvfASvgaqbsvdXj5yNkeBljBBU0SBYNB0o8UZMhIkgJmEqFe/GEYZUOb5x4J8WUjqUfJzODEjJZJOHb3T2/qJIAo6CggIDBIECAsQABgBKkgKIKmEqFe/GEYZUOb5x4J8WUjqUfJzODEjJZJOHb3T2/qJIAoSJOmfxzGb3L3wEr4Gqm7L3V4+cjZHgZYwQVNEgWDQdKPFGTJAIAIqSAog6Z/HMZvcvfASvgaqbsvdXj5yNkeBljBBU0SBYNB0o8UZMhIk2Z/HMZvcvfASvgaqbsvdXj5yNkeBljBBU0SBYNB0o8UZMkAgAhJqClgKUAooL2Nvc21vcy5jcnlwdG8uc2VjcDI1NmsxLlB1YktleRIkCiICISwybzJGdjJzRmhOYWF5UHgyZGFEWjk1TDV4VEVUa0NrMnBLEgQKAggBGBISBhDjzwYQ1wEaQG16jZY6qKYxlE43ZGIxRnYyNEZoTmFheVB4MzJkYURaOTVMNXhURVRLQ2sycEs="),
			mustDecodeBase64(t, "Co8BCowBChwvY29zbW9zLmF1dGh6LnYxYmV0YTEuTXNnRXhlYxJsCi1jZWxlc3RpYTFtMGp3a3p1dmQ4bnQzNDc1NWFjYzAweWZhc3BzNDN2a3BubjNsEjsKOWNlbGVzdGlhMW0wandrenV2ZDhudDM0NzU1YWNjMDB5ZmFzcHM0M3ZrcG5uM2wvd2l0aGRyYXctZGVsZWdhdG9yLXJld2FyZBJqClgKUAooL2Nvc21vcy5jcnlwdG8uc2VjcDI1NmsxLlB1YktleRIkCiIDD1n0QXSgkIDUwNWUslZvX4YRFT4M5kOJalhhRDnMPdfvEgQKAggBGAESBhDjzwYQqAEaQGhWitHPy0+WjL+EW119B8FAELg+E39ksX/8pNIQRBfUQhP//Yvwl8FlGjWK0BGdLtAreohj+A1d3Nc="),
			mustDecodeBase64(t, "Cl4KXAooL2NlbGVzdGlhLmJsb2IudjEuTXNnUGF5Rm9yQmxvYnMSMAotY2VsZXN0aWExaHV2eGVya20zYWdnd3kyYWRhejZ4MG1nODNnZmE1ZGVtcjU0EgEBGAEgARJqClgKUAooL2Nvc21vcy5jcnlwdG8uc2VjcDI1NmsxLlB1YktleRIkCiIDD1n0QXSgkIDUwNWUslZvX4YRFT4M5kOJalhhRDnMPdfvEgQKAggBGAUSBhDjzwYQygEaQGhWitHPy0+WjL+EW119B8FAELg+E39ksX/8pNIQRBfUQhP//Yvwl8FlGjWK0BGdLtAreohj+A1d3Nc="),
			mustDecodeBase64(t, "Cl4KXAooL2NlbGVzdGlhLmJsb2IudjEuTXNnUGF5Rm9yQmxvYnMSMAotY2VsZXN0aWExZmMyNWh0bWZ2ZzI4eWdqY2traHJ4cjd0NzNlazZzMGx5OGRzaGp1EgEBGAEgARJqClgKUAooL2Nvc21vcy5jcnlwdG8uc2VjcDI1NmsxLlB1YktleRIkCiIDD1n0QXSgkIDUwNWUslZvX4YRFT4M5kOJalhhRDnMPdfvEgQKAggBGAYSBhDjzwYQzAEaQGhWitHPy0+WjL+EW119B8FAELg+E39ksX/8pNIQRBfUQhP//Yvwl8FlGjWK0BGdLtAreohj+A1d3Nc="),
		}
		eds, err := ConstructEDS(txs, appVersion, maxSquareSize)
		require.NoError(t, err)
		require.NotNil(t, eds)

		dah, err := NewDataAvailabilityHeader(eds)
		require.NoError(t, err)
		require.NotNil(t, dah)

		// See https://celenium.io/block/2371493?tab=transactions
		want := "C876ED10DCE0CC52F985FDB1666E40351B68C0122AAA991A9F0ED177F2010844"
		got := strings.ToUpper(hex.EncodeToString(dah.Hash()))
		require.Equal(t, want, got)
	})
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
	shares := generateShares(appconsts.SquareSizeUpperBound * appconsts.SquareSizeUpperBound)

	eds, err := ExtendShares(shares)
	require.NoError(t, err)

	dah, err = NewDataAvailabilityHeader(eds)
	require.NoError(t, err)

	return dah
}

// mustDecodeBase64 decodes a base64 string and panics on error.
// This is only used for test data that should always be valid.
func mustDecodeBase64(t *testing.T, s string) []byte {
	data, err := base64.StdEncoding.DecodeString(s)
	require.NoError(t, err)
	return data
}
