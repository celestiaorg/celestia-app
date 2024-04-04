package ante_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	ante "github.com/celestiaorg/celestia-app/v2/x/blob/ante"
	blob "github.com/celestiaorg/celestia-app/v2/x/blob/types"
	"github.com/celestiaorg/go-square/shares"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	mebibyte   = 1_048_576 // 1 MiB
	squareSize = 64
)

func TestBlobShareDecorator(t *testing.T) {
	type testCase struct {
		name    string
		pfb     *blob.MsgPayForBlobs
		wantErr error
	}

	testCases := []testCase{
		{
			name: "PFB with 1 blob that is 1 byte",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{1},
			},
		},
		{
			name: "PFB with 1 blob that is 1 MiB",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{mebibyte},
			},
		},
		{
			name: "PFB with 1 blob that is 2 MiB",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{2 * mebibyte},
			},
			// This test case should return an error because a square size of 64
			// has exactly 2 MiB of total capacity so the total blob capacity
			// will be slightly smaller than 2 MiB.
			wantErr: blob.ErrBlobsTooLarge,
		},
		{
			name: "PFB with 2 blobs that are 1 byte each",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{1, 1},
			},
		},
		{
			name: "PFB with 2 blobs that are 1 MiB each",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{mebibyte, mebibyte},
			},
			// This test case should return an error for the same reason a
			// single blob that is 2 MiB returns an error.
			wantErr: blob.ErrBlobsTooLarge,
		},
		{
			name: "PFB with many single byte blobs should fit",
			pfb: &blob.MsgPayForBlobs{
				// 4095 blobs each of size 1 byte should occupy 4095 shares.
				// When square size is 64, there are 4095 shares available to
				// blob shares so we don't expect an error for this test case.
				BlobSizes: repeat(4095, 1),
			},
		},
		{
			name: "PFB with too many single byte blobs should not fit",
			pfb: &blob.MsgPayForBlobs{
				// 4096 blobs each of size 1 byte should occupy 4096 shares.
				// When square size is 64, there are 4095 shares available to
				// blob shares so we expect an error for this test case.
				BlobSizes: repeat(4096, 1),
			},
			wantErr: blob.ErrBlobsTooLarge,
		},
		{
			name: "PFB with 1 blob that is 1 share",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{uint32(shares.AvailableBytesFromSparseShares(1))},
			},
		},
		{
			name: "PFB with 1 blob that occupies total square - 1",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{uint32(shares.AvailableBytesFromSparseShares((squareSize * squareSize) - 1))},
			},
		},
		{
			name: "PFB with 1 blob that occupies total square",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{uint32(shares.AvailableBytesFromSparseShares(squareSize * squareSize))},
			},
			// This test case should return an error because if the blob
			// occupies the total square, there is no space for the PFB tx
			// share.
			wantErr: blob.ErrBlobsTooLarge,
		},
		{
			name: "PFB with 2 blobs that are 1 share each",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{
					uint32(shares.AvailableBytesFromSparseShares(1)),
					uint32(shares.AvailableBytesFromSparseShares(1)),
				},
			},
		},
		{
			name: "PFB with 2 blobs that occupy half the square each",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{
					uint32(shares.AvailableBytesFromSparseShares(squareSize * squareSize / 2)),
					uint32(shares.AvailableBytesFromSparseShares(squareSize * squareSize / 2)),
				},
			},
			wantErr: blob.ErrBlobsTooLarge,
		},
	}

	txConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...).TxConfig

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := sdk.Context{}.WithIsCheckTx(true)

			txBuilder := txConfig.NewTxBuilder()
			require.NoError(t, txBuilder.SetMsgs(tc.pfb))
			tx := txBuilder.GetTx()

			mbsd := ante.NewBlobShareDecorator(mockBlobKeeper{})
			_, err := mbsd.AnteHandle(ctx, tx, false, mockNext)
			assert.ErrorIs(t, tc.wantErr, err)
		})
	}
}

func mockNext(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
	return ctx, nil
}

// repeat returns a slice of length n with each element set to val.
func repeat(n int, val uint32) []uint32 {
	result := make([]uint32, n)
	for i := range result {
		result[i] = val
	}
	return result
}
