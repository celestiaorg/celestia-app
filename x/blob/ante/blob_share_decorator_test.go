package ante_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	v1 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v2"
	"github.com/celestiaorg/celestia-app/v2/pkg/user"
	"github.com/celestiaorg/celestia-app/v2/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v2/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	ante "github.com/celestiaorg/celestia-app/v2/x/blob/ante"
	blobtypes "github.com/celestiaorg/celestia-app/v2/x/blob/types"
	blob "github.com/celestiaorg/go-square/blob"
	"github.com/celestiaorg/go-square/shares"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	version "github.com/tendermint/tendermint/proto/tendermint/version"
)

const (
	mebibyte   = 1_048_576 // 1 MiB
	squareSize = 64
)

func TestBlobShareDecorator(t *testing.T) {
	type testCase struct {
		name        string
		blobsPerPFB int
		blobSize    int
		appVersion  uint64
		wantErr     error
	}

	rand := tmrand.NewRand()

	testCases := []testCase{
		{
			name:        "want no error if appVersion v1 and 8 MiB blob",
			blobsPerPFB: 1,
			blobSize:    8 * mebibyte,
			appVersion:  v1.Version,
		},
		{
			name:        "PFB with 1 blob that is 1 byte",
			blobsPerPFB: 1,
			blobSize:    1,
			appVersion:  v2.Version,
		},
		{
			name:        "PFB with 1 blob that is 1 MiB",
			blobsPerPFB: 1,
			blobSize:    1 * mebibyte,
			appVersion:  v2.Version,
		},
		{
			name:        "PFB with 1 blob that is 2 MiB",
			blobsPerPFB: 1,
			blobSize:    2 * mebibyte,
			appVersion:  v2.Version,
			// This test case should return an error because a square size of 64
			// has exactly 2 MiB of total capacity so the total blob capacity
			// will be slightly smaller than 2 MiB.
			wantErr: blobtypes.ErrBlobsTooLarge,
		},
		{
			name:        "PFB with 2 blobs that are 1 byte each",
			blobsPerPFB: 2,
			blobSize:    1,
			appVersion:  v2.Version,
		},
		{
			name:        "PFB with 2 blobs that are 1 MiB each",
			blobsPerPFB: 2,
			blobSize:    1 * mebibyte,
			appVersion:  v2.Version,
			// This test case should return an error for the same reason a
			// single blob that is 2 MiB returns an error.
			wantErr: blobtypes.ErrBlobsTooLarge,
		},
		{
			name:        "PFB with many single byte blobs should fit",
			blobsPerPFB: 3000,
			blobSize:    1,
			appVersion:  v2.Version,
		},
		{
			name:        "PFB with too many single byte blobs should not fit",
			blobsPerPFB: 4000,
			blobSize:    1,
			appVersion:  v2.Version,
			wantErr:     blobtypes.ErrBlobsTooLarge,
		},
		{
			name:        "PFB with 1 blob that is 1 share",
			blobsPerPFB: 1,
			blobSize:    100,
			appVersion:  v2.Version,
		},
		{
			name:        "PFB with 1 blob that occupies total square - 1",
			blobsPerPFB: 1,
			blobSize:    shares.AvailableBytesFromSparseShares(squareSize*squareSize - 1),
			appVersion:  v2.Version,
		},
		{
			name:        "PFB with 1 blob that occupies total square",
			blobsPerPFB: 1,
			blobSize:    shares.AvailableBytesFromSparseShares(squareSize * squareSize),
			appVersion:  v2.Version,
			// This test case should return an error because if the blob
			// occupies the total square, there is no space for the PFB tx
			// share.
			wantErr: blobtypes.ErrBlobsTooLarge,
		},
		{
			name:        "PFB with 2 blobs that are 1 share each",
			blobsPerPFB: 2,
			blobSize:    100,
			appVersion:  v2.Version,
		},
		{
			name:        "PFB with 2 blobs that occupy half the square each",
			blobsPerPFB: 2,
			blobSize:    shares.AvailableBytesFromSparseShares(squareSize * squareSize / 2),
			appVersion:  v2.Version,
			wantErr:     blobtypes.ErrBlobsTooLarge,
		},
	}

	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			kr, _ := testnode.NewKeyring(testfactory.TestAccName)
			signer, err := user.NewSigner(
				kr,
				ecfg.TxConfig,
				testfactory.ChainID,
				tc.appVersion,
				user.NewAccount(testfactory.TestAccName, 1, 0),
			)
			require.NoError(t, err)

			blobTx := blobfactory.RandBlobTxs(signer, rand, 1, tc.blobsPerPFB, tc.blobSize)

			btx, isBlob := blob.UnmarshalBlobTx([]byte(blobTx[0]))
			require.True(t, isBlob)

			sdkTx, err := ecfg.TxConfig.TxDecoder()(btx.Tx)
			require.NoError(t, err)

			decorator := ante.NewBlobShareDecorator(mockBlobKeeper{})
			ctx := sdk.Context{}.
				WithIsCheckTx(true).
				WithBlockHeader(tmproto.Header{Version: version.Consensus{App: tc.appVersion}}).
				WithTxBytes(btx.Tx)
			_, err = decorator.AnteHandle(ctx, sdkTx, false, mockNext)
			assert.ErrorIs(t, tc.wantErr, err)
		})
	}
}

func mockNext(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
	return ctx, nil
}
