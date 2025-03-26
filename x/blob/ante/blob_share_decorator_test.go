package ante_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/go-square/v2/share"
	blobtx "github.com/celestiaorg/go-square/v2/tx"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/pkg/user"
	"github.com/celestiaorg/celestia-app/v4/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/random"
	"github.com/celestiaorg/celestia-app/v4/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
	ante "github.com/celestiaorg/celestia-app/v4/x/blob/ante"
	blob "github.com/celestiaorg/celestia-app/v4/x/blob/types"
)

const (
	mebibyte   = 1_048_576 // 1 MiB
	squareSize = 64
)

func TestBlobShareDecorator(t *testing.T) {
	type testCase struct {
		name                  string
		blobsPerPFB, blobSize int
		wantErr               error
	}

	rand := random.New()
	enc := encoding.MakeTestConfig(app.ModuleEncodingRegisters...)

	testCases := []testCase{
		{
			name:        "PFB with 1 blob that is 1 byte",
			blobsPerPFB: 1,
			blobSize:    1,
		},
		{
			name:        "PFB with 1 blob that is 1 MiB",
			blobsPerPFB: 1,
			blobSize:    1 * mebibyte,
		},
		{
			name:        "PFB with 1 blob that is 2 MiB",
			blobsPerPFB: 1,
			blobSize:    2 * mebibyte,
			// This test case should return an error because a square size of 64
			// has exactly 2 MiB of total capacity so the total blob capacity
			// will be slightly smaller than 2 MiB.
			wantErr: blob.ErrBlobsTooLarge,
		},
		{
			name:        "PFB with 2 blobs that are 1 byte each",
			blobsPerPFB: 2,
			blobSize:    1,
		},
		{
			name:        "PFB with 2 blobs that are 1 MiB each",
			blobsPerPFB: 2,
			blobSize:    1 * mebibyte,
			// This test case should return an error for the same reason a
			// single blob that is 2 MiB returns an error.
			wantErr: blob.ErrBlobsTooLarge,
		},
		{
			name:        "PFB with many single byte blobs should fit",
			blobsPerPFB: 3000,
			blobSize:    1,
		},
		{
			name:        "PFB with too many single byte blobs should not fit",
			blobsPerPFB: 4000,
			blobSize:    1,
			wantErr:     blob.ErrBlobsTooLarge,
		},
		{
			name:        "PFB with 1 blob that is 1 share",
			blobsPerPFB: 1,
			blobSize:    100,
		},
		{
			name:        "PFB with 1 blob that occupies total square - 1",
			blobsPerPFB: 1,
			blobSize:    share.AvailableBytesFromSparseShares(squareSize*squareSize - 1),
		},
		{
			name:        "PFB with 1 blob that occupies total square",
			blobsPerPFB: 1,
			blobSize:    share.AvailableBytesFromSparseShares(squareSize * squareSize),
			// This test case should return an error because if the blob
			// occupies the total square, there is no space for the PFB tx
			// share.
			wantErr: blob.ErrBlobsTooLarge,
		},
		{
			name:        "PFB with 2 blobs that are 1 share each",
			blobsPerPFB: 2,
			blobSize:    100,
		},
		{
			name:        "PFB with 2 blobs that occupy half the square each",
			blobsPerPFB: 2,
			blobSize:    share.AvailableBytesFromSparseShares(squareSize * squareSize / 2),
			wantErr:     blob.ErrBlobsTooLarge,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			kr, _ := testnode.NewKeyring(testfactory.TestAccName)
			signer, err := user.NewSigner(kr, enc.TxConfig, testfactory.ChainID, user.NewAccount(testfactory.TestAccName, 1, 0))
			require.NoError(t, err)

			blobTx := blobfactory.RandBlobTxs(signer, rand, 1, tc.blobsPerPFB, tc.blobSize)

			btx, isBlob, err := blobtx.UnmarshalBlobTx([]byte(blobTx[0]))
			require.NoError(t, err)
			require.True(t, isBlob)

			sdkTx, err := enc.TxConfig.TxDecoder()(btx.Tx)
			require.NoError(t, err)

			decorator := ante.NewBlobShareDecorator(mockBlobKeeper{})
			ctx := sdk.Context{}.
				WithIsCheckTx(true).
				WithTxBytes(btx.Tx)
			_, err = decorator.AnteHandle(ctx, sdkTx, false, mockNext)
			assert.ErrorIs(t, tc.wantErr, err)
		})
	}
}

func mockNext(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
	return ctx, nil
}
