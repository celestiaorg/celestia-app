package ante_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	ante "github.com/celestiaorg/celestia-app/v3/x/blob/ante"
	blob "github.com/celestiaorg/celestia-app/v3/x/blob/types"
	"github.com/celestiaorg/go-square/shares"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

const (
	testGasPerBlobByte   = 10
	testGovMaxSquareSize = 64
)

func TestPFBAnteHandler(t *testing.T) {
	txConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...).TxConfig
	testCases := []struct {
		name        string
		pfb         *blob.MsgPayForBlobs
		txGas       uint64
		gasConsumed uint64
		wantErr     bool
	}{
		{
			name: "valid pfb single blob",
			pfb: &blob.MsgPayForBlobs{
				// 1 share = 512 bytes = 5120 gas
				BlobSizes: []uint32{uint32(shares.AvailableBytesFromSparseShares(1))},
			},
			txGas:       appconsts.ShareSize * testGasPerBlobByte,
			gasConsumed: 0,
			wantErr:     false,
		},
		{
			name: "valid pfb multi blob",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{uint32(shares.AvailableBytesFromSparseShares(1)), uint32(shares.AvailableBytesFromSparseShares(2))},
			},
			txGas:       3 * appconsts.ShareSize * testGasPerBlobByte,
			gasConsumed: 0,
			wantErr:     false,
		},
		{
			name: "pfb single blob not enough gas",
			pfb: &blob.MsgPayForBlobs{
				// 2 share = 1024 bytes = 10240 gas
				BlobSizes: []uint32{uint32(shares.AvailableBytesFromSparseShares(1) + 1)},
			},
			txGas:       2*appconsts.ShareSize*testGasPerBlobByte - 1,
			gasConsumed: 0,
			wantErr:     true,
		},
		{
			name: "pfb mulit blob not enough gas",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{uint32(shares.AvailableBytesFromSparseShares(1)), uint32(shares.AvailableBytesFromSparseShares(2))},
			},
			txGas:       3*appconsts.ShareSize*testGasPerBlobByte - 1,
			gasConsumed: 0,
			wantErr:     true,
		},
		{
			name: "pfb with existing gas consumed",
			pfb: &blob.MsgPayForBlobs{
				// 1 share = 512 bytes = 5120 gas
				BlobSizes: []uint32{uint32(shares.AvailableBytesFromSparseShares(1))},
			},
			txGas:       appconsts.ShareSize*testGasPerBlobByte + 10000 - 1,
			gasConsumed: 10000,
			wantErr:     true,
		},
		{
			name: "valid pfb with existing gas consumed",
			pfb: &blob.MsgPayForBlobs{
				// 1 share = 512 bytes = 5120 gas
				BlobSizes: []uint32{uint32(shares.AvailableBytesFromSparseShares(10))},
			},
			txGas:       1000000,
			gasConsumed: 10000,
			wantErr:     false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			anteHandler := ante.NewMinGasPFBDecorator(mockBlobKeeper{})
			ctx := sdk.Context{}.WithGasMeter(sdk.NewGasMeter(tc.txGas)).WithIsCheckTx(true)
			ctx.GasMeter().ConsumeGas(tc.gasConsumed, "test")
			txBuilder := txConfig.NewTxBuilder()
			require.NoError(t, txBuilder.SetMsgs(tc.pfb))
			tx := txBuilder.GetTx()
			_, err := anteHandler.AnteHandle(ctx, tx, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil })
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

type mockBlobKeeper struct{}

func (mockBlobKeeper) GasPerBlobByte(_ sdk.Context) uint32 {
	return testGasPerBlobByte
}

func (mockBlobKeeper) GovMaxSquareSize(_ sdk.Context) uint64 {
	return testGovMaxSquareSize
}
