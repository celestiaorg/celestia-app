package ante_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	v1 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v2"
	"github.com/celestiaorg/go-square/shares"
	sdk "github.com/cosmos/cosmos-sdk/types"
	ante "github.com/rootulp/celestia-app/x/blob/ante"
	blob "github.com/rootulp/celestia-app/x/blob/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	version "github.com/tendermint/tendermint/proto/tendermint/version"
)

const (
	mebibyte   = 1_048_576 // 1 MiB
	squareSize = 64
)

func TestBlobShareDecorator(t *testing.T) {
	type testCase struct {
		name       string
		pfb        *blob.MsgPayForBlobs
		appVersion uint64
		wantErr    error
	}

	testCases := []testCase{
		{
			name: "want no error if appVersion v1 and 8 MiB blob",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{8 * mebibyte},
			},
			appVersion: v1.Version,
		},
		{
			name: "PFB with 1 blob that is 1 byte",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{1},
			},
			appVersion: v2.Version,
		},
		{
			name: "PFB with 1 blob that is 1 MiB",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{mebibyte},
			},
			appVersion: v2.Version,
		},
		{
			name: "PFB with 1 blob that is 2 MiB",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{2 * mebibyte},
			},
			appVersion: v2.Version,
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
			appVersion: v2.Version,
		},
		{
			name: "PFB with 2 blobs that are 1 MiB each",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{mebibyte, mebibyte},
			},
			appVersion: v2.Version,
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
			appVersion: v2.Version,
		},
		{
			name: "PFB with too many single byte blobs should not fit",
			pfb: &blob.MsgPayForBlobs{
				// 4096 blobs each of size 1 byte should occupy 4096 shares.
				// When square size is 64, there are 4095 shares available to
				// blob shares so we expect an error for this test case.
				BlobSizes: repeat(4096, 1),
			},
			appVersion: v2.Version,
			wantErr:    blob.ErrBlobsTooLarge,
		},
		{
			name: "PFB with 1 blob that is 1 share",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{uint32(shares.AvailableBytesFromSparseShares(1))},
			},
			appVersion: v2.Version,
		},
		{
			name: "PFB with 1 blob that occupies total square - 1",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{uint32(shares.AvailableBytesFromSparseShares((squareSize * squareSize) - 1))},
			},
			appVersion: v2.Version,
		},
		{
			name: "PFB with 1 blob that occupies total square",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{uint32(shares.AvailableBytesFromSparseShares(squareSize * squareSize))},
			},
			appVersion: v2.Version,
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
			appVersion: v2.Version,
		},
		{
			name: "PFB with 2 blobs that occupy half the square each",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{
					uint32(shares.AvailableBytesFromSparseShares(squareSize * squareSize / 2)),
					uint32(shares.AvailableBytesFromSparseShares(squareSize * squareSize / 2)),
				},
			},
			appVersion: v2.Version,
			wantErr:    blob.ErrBlobsTooLarge,
		},
	}

	txConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...).TxConfig
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			txBuilder := txConfig.NewTxBuilder()
			require.NoError(t, txBuilder.SetMsgs(tc.pfb))
			tx := txBuilder.GetTx()

			decorator := ante.NewBlobShareDecorator(mockBlobKeeper{})
			ctx := sdk.Context{}.WithIsCheckTx(true).WithBlockHeader(tmproto.Header{Version: version.Consensus{App: tc.appVersion}})
			_, err := decorator.AnteHandle(ctx, tx, false, mockNext)
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
