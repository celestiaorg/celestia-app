package ante_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	ante "github.com/celestiaorg/celestia-app/x/blob/ante"
	blob "github.com/celestiaorg/celestia-app/x/blob/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	mebibyte = 1_048_576 // 1 MiB
)

func TestMaxTotalBlobSizeAnteHandler(t *testing.T) {
	type testCase struct {
		name    string
		pfb     *blob.MsgPayForBlobs
		wantErr bool
	}

	testCases := []testCase{
		// tests based on bytes
		{
			name: "PFB with 1 blob that is 1 byte",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{1},
			},
			wantErr: false,
		},
		{
			name: "PFB with 1 blob that is 1 MiB",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{mebibyte},
			},
			wantErr: false,
		},
		{
			name: "PFB with 1 blob that is 2 MiB",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{2 * mebibyte},
			},
			// This test case should return an error because a square size of 64
			// has exactly 2 MiB of total capacity so the total blob capacity
			// will be slightly smaller than 2 MiB.
			wantErr: true,
		},
		{
			name: "PFB with 2 blobs that are 1 byte each",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{1, 1},
			},
			wantErr: false,
		},
		{
			name: "PFB with 2 blobs that are 1 MiB each",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{mebibyte, mebibyte},
			},
			// This test case should return an error for the same reason a
			// single blob that is 2 MiB returns an error.
			wantErr: true,
		},
		// tests based on shares
		{
			name: "PFB with 1 blob that is 1 share",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{uint32(shares.AvailableBytesFromSparseShares(1))},
			},
			wantErr: false,
		},
		{
			name: "PFB with 1 blob that is 4,095 shares",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{uint32(shares.AvailableBytesFromSparseShares(4095))},
			},
			wantErr: false,
		},
		{
			name: "PFB with 1 blob that is 4,096 shares",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{uint32(shares.AvailableBytesFromSparseShares(4096))},
			},
			// This test case should return an error because max square size is
			// 64 * 64 = 4,096. If the blob occupies 4,096 shares then there is
			// no space for the PFB tx share.
			wantErr: true,
		},
		{
			name: "PFB with 2 blobs that are 1 share each",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{
					uint32(shares.AvailableBytesFromSparseShares(1)),
					uint32(shares.AvailableBytesFromSparseShares(1)),
				},
			},
			wantErr: false,
		},
		{
			name: "PFB with 2 blobs that are 2,048 shares each",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{
					uint32(shares.AvailableBytesFromSparseShares(2048)),
					uint32(shares.AvailableBytesFromSparseShares(2048)),
				},
			},
			wantErr: true,
		},
	}

	txConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...).TxConfig

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := sdk.Context{}.WithIsCheckTx(true)

			txBuilder := txConfig.NewTxBuilder()
			require.NoError(t, txBuilder.SetMsgs(tc.pfb))
			tx := txBuilder.GetTx()

			mbsd := ante.NewMaxBlobSizeDecorator(mockBlobKeeper{})
			_, err := mbsd.AnteHandle(ctx, tx, false, mockNext)

			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func mockNext(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
	return ctx, nil
}
