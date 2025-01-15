package ante_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	v1 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v2"
	ante "github.com/celestiaorg/celestia-app/v3/x/blob/ante"
	blob "github.com/celestiaorg/celestia-app/v3/x/blob/types"
	"github.com/celestiaorg/go-square/v2/share"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	version "github.com/tendermint/tendermint/proto/tendermint/version"
)

func TestMaxTotalBlobSizeDecorator(t *testing.T) {
	type testCase struct {
		name       string
		pfb        *blob.MsgPayForBlobs
		appVersion uint64
		wantErr    error
	}

	testCases := []testCase{
		{
			name: "want no error if appVersion v2 and 8 MiB blob",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{1},
			},
			appVersion: v2.Version,
		},
		{
			name: "PFB with 1 blob that is 1 byte",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{1},
			},
			appVersion: v1.Version,
		},
		{
			name: "PFB with 1 blob that is 1 MiB",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{Mebibyte},
			},
			appVersion: v1.Version,
		},
		{
			name: "PFB with 1 blob that is 2 MiB",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{2 * Mebibyte},
			},
			appVersion: v1.Version,
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
			appVersion: v1.Version,
		},
		{
			name: "PFB with 2 blobs that are 1 MiB each",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{Mebibyte, Mebibyte},
			},
			appVersion: v1.Version,
			// This test case should return an error for the same reason a
			// single blob that is 2 MiB returns an error.
			wantErr: blob.ErrTotalBlobSizeTooLarge,
		},
		{
			name: "PFB with 1 blob that is 1 share",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{uint32(share.AvailableBytesFromSparseShares(1))},
			},
			appVersion: v1.Version,
		},
		{
			name: "PFB with 1 blob that occupies total square - 1",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{uint32(share.AvailableBytesFromSparseShares((squareSize * squareSize) - 1))},
			},
			appVersion: v1.Version,
		},
		{
			name: "PFB with 1 blob that occupies total square",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{uint32(share.AvailableBytesFromSparseShares(squareSize * squareSize))},
			},
			appVersion: v1.Version,
			// This test case should return an error because if the blob
			// occupies the total square, there is no space for the PFB tx
			// share.
			wantErr: blob.ErrTotalBlobSizeTooLarge,
		},
		{
			name: "PFB with 2 blobs that are 1 share each",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{
					uint32(share.AvailableBytesFromSparseShares(1)),
					uint32(share.AvailableBytesFromSparseShares(1)),
				},
			},
			appVersion: v1.Version,
		},
		{
			name: "PFB with 2 blobs that occupy half the square each",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{
					uint32(share.AvailableBytesFromSparseShares(squareSize * squareSize / 2)),
					uint32(share.AvailableBytesFromSparseShares(squareSize * squareSize / 2)),
				},
			},
			appVersion: v1.Version,
			wantErr:    blob.ErrTotalBlobSizeTooLarge,
		},
	}

	txConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...).TxConfig
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			txBuilder := txConfig.NewTxBuilder()
			require.NoError(t, txBuilder.SetMsgs(tc.pfb))
			tx := txBuilder.GetTx()

			decorator := ante.NewMaxTotalBlobSizeDecorator(mockBlobKeeper{})
			ctx := sdk.Context{}.WithIsCheckTx(true).WithBlockHeader(tmproto.Header{Version: version.Consensus{App: tc.appVersion}})
			_, err := decorator.AnteHandle(ctx, tx, false, mockNext)
			assert.ErrorIs(t, tc.wantErr, err)
		})
	}
}
