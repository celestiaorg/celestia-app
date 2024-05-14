package ante_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	v1 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v1"
	ante "github.com/celestiaorg/celestia-app/v2/x/blob/ante"
	blob "github.com/celestiaorg/celestia-app/v2/x/blob/types"
	"github.com/celestiaorg/go-square/shares"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	version "github.com/tendermint/tendermint/proto/tendermint/version"
)

func TestMaxTotalBlobSizeDecorator(t *testing.T) {
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
			wantErr: blob.ErrTotalBlobSizeTooLarge,
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
			wantErr: blob.ErrTotalBlobSizeTooLarge,
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
			wantErr: blob.ErrTotalBlobSizeTooLarge,
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
			wantErr: blob.ErrTotalBlobSizeTooLarge,
		},
	}

	txConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...).TxConfig
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := sdk.Context{}.WithIsCheckTx(true).WithBlockHeader(tmproto.Header{Version: version.Consensus{App: v1.Version}})
			txBuilder := txConfig.NewTxBuilder()
			require.NoError(t, txBuilder.SetMsgs(tc.pfb))
			tx := txBuilder.GetTx()

			decorator := ante.NewMaxBlobSizeDecorator(mockBlobKeeper{})
			_, err := decorator.AnteHandle(ctx, tx, false, mockNext)
			assert.ErrorIs(t, tc.wantErr, err)
		})
	}
}
