package da

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	appconstsv5 "github.com/celestiaorg/celestia-app/v10/pkg/appconsts/v5"
	"github.com/celestiaorg/celestia-app/v10/pkg/wrapper"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	sh "github.com/celestiaorg/go-square/v4/share"
	squaretx "github.com/celestiaorg/go-square/v4/tx"
	"github.com/celestiaorg/rsmt2d"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cosmostx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/stretchr/testify/require"
)

// TestDataRootCompatibilityVectors pins data roots for fixed squares. Each root
// is computed with fresh trees, a fresh tree pool, and a tree pool that is
// reused across square sizes and has just failed a root computation.
func TestDataRootCompatibilityVectors(t *testing.T) {
	// emptyRoot equals the hash pinned in TestMinDataAvailabilityHeader.
	const emptyRoot = "3d96b7d238e7e0456f6af8e7cdf0a67bd6cf9c2089ecb559c659dcaa1f880353"
	sdkTx := bytes.Repeat([]byte{0x01}, 200)
	blobTx := newBlobTx(t)
	pffTx := buildMsgPayForFibreTxBytes(t)

	type testCase struct {
		name       string
		shares     [][]byte
		squareSize int
		want       string
	}
	testCases := []testCase{
		{
			name:       "empty block v5",
			shares:     mustConstructShares(t, nil, appconstsv5.Version),
			squareSize: 1,
			want:       emptyRoot,
		},
		{
			name:       "empty block v7",
			shares:     mustConstructShares(t, nil, 7),
			squareSize: 1,
			want:       emptyRoot,
		},
		{
			name:       "empty block latest",
			shares:     mustConstructShares(t, nil, appconsts.Version),
			squareSize: 1,
			want:       emptyRoot,
		},
		{
			name:       "large square",
			shares:     generateShares(128 * 128),
			squareSize: 128,
			want:       "0bd3abeeacfbb0b92dfbdac4a154868e3c4e79666f7fcf6c620bb90dd3a0dcf0",
		},
		{
			name:       "min square with one sdk tx",
			shares:     mustConstructShares(t, [][]byte{sdkTx}, appconsts.Version),
			squareSize: 1,
			want:       "0dfed78da1fbdfba31299b40b955639a10915963e5b9bd59d299b559f1e457ee",
		},
		{
			name:       "sdk and pfb txs with namespace padding v5",
			shares:     mustConstructShares(t, [][]byte{sdkTx, blobTx}, appconstsv5.Version),
			squareSize: 16,
			want:       "04d4918f2b61f007b8177ea9f77c99befc197af9265d02615397154e5c37b7c9",
		},
		{
			name:       "sdk, pfb and pff txs with reserved padding latest",
			shares:     mustConstructShares(t, [][]byte{sdkTx, blobTx, pffTx}, appconsts.Version),
			squareSize: 16,
			want:       "8476633766ca2fe7211d7f05c6d2276c240aac88b99f8c09278d94622ba3eae2",
		},
	}

	reusedPool, err := wrapper.NewTreePool(1, 2)
	require.NoError(t, err)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			freshPool, err := wrapper.NewTreePool(uint(tc.squareSize), 2)
			require.NoError(t, err)
			_, err = dataRoot(descendingRowSquare(), withPool(reusedPool))
			require.Error(t, err)

			fresh, err := dataRoot(tc.shares, ExtendShares)
			require.NoError(t, err)
			require.Len(t, fresh.RowRoots, 2*tc.squareSize)
			require.Equal(t, tc.want, hex.EncodeToString(fresh.Hash()))

			pooled, err := dataRoot(tc.shares, withPool(freshPool))
			require.NoError(t, err)
			require.True(t, fresh.Equals(&pooled), "fresh pool")

			reused, err := dataRoot(tc.shares, withPool(reusedPool))
			require.NoError(t, err)
			require.True(t, fresh.Equals(&reused), "reused pool after failure")
		})
	}
}

func TestNewDataAvailabilityHeaderRootErrors(t *testing.T) {
	pool, err := wrapper.NewTreePool(2, 2)
	require.NoError(t, err)
	testCases := []struct {
		name   string
		shares [][]byte
	}{
		{name: "row namespaces out of order", shares: descendingRowSquare()},
		{name: "column namespaces out of order", shares: descendingColumnSquare()},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, extend := range []extendFunc{ExtendShares, withPool(pool)} {
				_, err := dataRoot(tc.shares, extend)
				require.ErrorContains(t, err, "lexicographically ordered")
			}
		})
	}
}

func TestConstructSharesErrors(t *testing.T) {
	const maxSquareSize = 2
	tooBigTx := bytes.Repeat([]byte{0x01}, sh.AvailableBytesFromCompactShares(maxSquareSize*maxSquareSize)+1)
	sdkTx := bytes.Repeat([]byte{0x01}, 200)

	type testCase struct {
		name       string
		txs        [][]byte
		appVersion uint64
	}
	testCases := []testCase{
		{name: "tx exceeds max square v5", txs: [][]byte{tooBigTx}, appVersion: appconstsv5.Version},
		{name: "tx exceeds max square v7", txs: [][]byte{tooBigTx}, appVersion: 7},
		{name: "tx exceeds max square latest", txs: [][]byte{tooBigTx}, appVersion: appconsts.Version},
		{name: "pff tx before sdk tx", txs: [][]byte{buildMsgPayForFibreTxBytes(t), sdkTx}, appVersion: appconsts.Version},
		{name: "malformed pff tx", txs: [][]byte{malformedPayForFibreTx(t)}, appVersion: appconsts.Version},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := constructShares(tc.txs, tc.appVersion, maxSquareSize)
			require.Error(t, err)
		})
	}
}

func TestDataAvailabilityHeaderIsZero(t *testing.T) {
	minDAH := MinDataAvailabilityHeader()
	require.True(t, (*DataAvailabilityHeader)(nil).IsZero())
	require.True(t, new(DataAvailabilityHeader).IsZero())
	require.True(t, (&DataAvailabilityHeader{RowRoots: minDAH.RowRoots}).IsZero())
	require.False(t, minDAH.IsZero())
}

func TestDataAvailabilityHeaderNil(t *testing.T) {
	var dah *DataAvailabilityHeader
	require.Error(t, dah.ValidateBasic())
	_, err := dah.ToProto()
	require.Error(t, err)
	_, err = DataAvailabilityHeaderFromProto(nil)
	require.Error(t, err)
}

// dataRoot extends shares and returns the resulting data availability header.
func dataRoot(shares [][]byte, extend extendFunc) (DataAvailabilityHeader, error) {
	eds, err := extend(shares)
	if err != nil {
		return DataAvailabilityHeader{}, err
	}
	return NewDataAvailabilityHeader(eds)
}

func withPool(pool *wrapper.TreePool) extendFunc {
	return func(s [][]byte) (*rsmt2d.ExtendedDataSquare, error) {
		return ExtendSharesWithTreePool(s, pool)
	}
}

func mustConstructShares(t *testing.T, txs [][]byte, appVersion uint64) [][]byte {
	t.Helper()
	shares, err := constructShares(txs, appVersion, -1)
	require.NoError(t, err)
	return shares
}

// newBlobTx returns a BlobTx with two blobs in different namespaces. Each blob
// spans more than SubtreeRootThreshold shares, so the square needs padding to
// align them.
func newBlobTx(t *testing.T) []byte {
	t.Helper()
	blobLen := sh.AvailableBytesFromSparseShares(appconsts.SubtreeRootThreshold + 1)
	ns1 := sh.MustNewV0Namespace(bytes.Repeat([]byte{1}, sh.NamespaceVersionZeroIDSize))
	ns2 := sh.MustNewV0Namespace(bytes.Repeat([]byte{2}, sh.NamespaceVersionZeroIDSize))
	blob1, err := sh.NewBlob(ns1, bytes.Repeat([]byte{0xAA}, blobLen), sh.ShareVersionZero, nil)
	require.NoError(t, err)
	blob2, err := sh.NewBlob(ns2, bytes.Repeat([]byte{0xBB}, blobLen), sh.ShareVersionZero, nil)
	require.NoError(t, err)
	blobTx, err := squaretx.MarshalBlobTx([]byte("pfb"), blob1, blob2)
	require.NoError(t, err)
	return blobTx
}

// malformedPayForFibreTx returns an SDK tx whose MsgPayForFibre cannot be
// decoded.
func malformedPayForFibreTx(t *testing.T) []byte {
	t.Helper()
	tx := &cosmostx.Tx{Body: &cosmostx.TxBody{Messages: []*codectypes.Any{{
		TypeUrl: fibretypes.MsgPayForFibreTypeURL,
		Value:   []byte{0xFF},
	}}}}
	txBytes, err := tx.Marshal()
	require.NoError(t, err)
	return txBytes
}

// descendingRowSquare returns a 2x2 square whose first row has descending
// namespaces.
func descendingRowSquare() [][]byte {
	return [][]byte{shareWithNamespaceID(2), shareWithNamespaceID(1), shareWithNamespaceID(3), shareWithNamespaceID(4)}
}

// descendingColumnSquare returns a 2x2 square whose rows are ordered but whose
// first column has descending namespaces.
func descendingColumnSquare() [][]byte {
	return [][]byte{shareWithNamespaceID(2), shareWithNamespaceID(3), shareWithNamespaceID(1), shareWithNamespaceID(4)}
}

func shareWithNamespaceID(id byte) []byte {
	ns := sh.MustNewV0Namespace(bytes.Repeat([]byte{id}, sh.NamespaceVersionZeroIDSize))
	return generateShare(ns.Bytes())
}
