package ante_test

import (
	"math"
	"testing"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/x/blob/ante"
	blob "github.com/celestiaorg/celestia-app/v6/x/blob/types"
	"github.com/celestiaorg/go-square/v2/share"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cometbft/cometbft/proto/tendermint/version"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/stretchr/testify/require"
)

const (
	testGasPerBlobByte   = 10
	testGovMaxSquareSize = 64
)

func TestPFBAnteHandler(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	testCases := []struct {
		name        string
		pfb         *blob.MsgPayForBlobs
		txGas       func(uint32) uint32
		gasConsumed uint64
		wantErr     bool
	}{
		{
			name: "valid pfb single blob",
			pfb: &blob.MsgPayForBlobs{
				// 1 share = 512 bytes = 5120 gas
				BlobSizes: []uint32{uint32(share.AvailableBytesFromSparseShares(1))},
			},
			txGas: func(testGasPerBlobByte uint32) uint32 {
				return share.ShareSize * testGasPerBlobByte
			},
			gasConsumed: 0,
			wantErr:     false,
		},
		{
			name: "valid pfb multi blob",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{uint32(share.AvailableBytesFromSparseShares(1)), uint32(share.AvailableBytesFromSparseShares(2))},
			},
			txGas: func(testGasPerBlobByte uint32) uint32 {
				return 3 * share.ShareSize * testGasPerBlobByte
			},
			gasConsumed: 0,
			wantErr:     false,
		},
		{
			name: "pfb single blob not enough gas",
			pfb: &blob.MsgPayForBlobs{
				// 2 share = 1024 bytes = 10240 gas
				BlobSizes: []uint32{uint32(share.AvailableBytesFromSparseShares(1) + 1)},
			},
			txGas: func(testGasPerBlobByte uint32) uint32 {
				return 2*share.ShareSize*testGasPerBlobByte - 1
			},
			gasConsumed: 0,
			wantErr:     true,
		},
		{
			name: "pfb multi blob not enough gas",
			pfb: &blob.MsgPayForBlobs{
				BlobSizes: []uint32{uint32(share.AvailableBytesFromSparseShares(1)), uint32(share.AvailableBytesFromSparseShares(2))},
			},
			txGas: func(testGasPerBlobByte uint32) uint32 {
				return 3*share.ShareSize*testGasPerBlobByte - 1
			},
			gasConsumed: 0,
			wantErr:     true,
		},
		{
			name: "pfb with existing gas consumed",
			pfb: &blob.MsgPayForBlobs{
				// 1 share = 512 bytes = 5120 gas
				BlobSizes: []uint32{uint32(share.AvailableBytesFromSparseShares(1))},
			},
			txGas: func(testGasPerBlobByte uint32) uint32 {
				return share.ShareSize*testGasPerBlobByte + 10000 - 1
			},
			gasConsumed: 10000,
			wantErr:     true,
		},
		{
			name: "valid pfb with existing gas consumed",
			pfb: &blob.MsgPayForBlobs{
				// 1 share = 512 bytes = 5120 gas
				BlobSizes: []uint32{uint32(share.AvailableBytesFromSparseShares(10))},
			},
			txGas: func(_ uint32) uint32 {
				return 1000000
			},
			gasConsumed: 10000,
			wantErr:     false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			anteHandler := ante.NewMinGasPFBDecorator(mockBlobKeeper{})
			header := tmproto.Header{
				Height: 1,
				Version: version.Consensus{
					App: appconsts.Version,
				},
			}
			ctx := sdk.NewContext(nil, header, true, log.NewNopLogger()).
				WithGasMeter(storetypes.NewGasMeter(uint64(tc.txGas(appconsts.GasPerBlobByte)))).
				WithIsCheckTx(true)

			ctx.GasMeter().ConsumeGas(tc.gasConsumed, "test")
			txBuilder := enc.TxConfig.NewTxBuilder()
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

// TestMinGasPFBDecoratorWithMsgExec tests that the MinGasPFBDecorator rejects a
// MsgExec that contains a MsgExec with a MsgPayForBlob where the MsgPayForBlob
// gas cost is greater than the tx's gas limit.
func TestMinGasPFBDecoratorWithMsgExec(t *testing.T) {
	anteHandler := ante.NewMinGasPFBDecorator(mockBlobKeeper{})
	txConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...).TxConfig

	// Create a context with a gas meter with a high gas limit
	gasLimit := uint64(100_000_000)
	ctx := sdk.NewContext(nil, tmproto.Header{
		Version: version.Consensus{
			App: appconsts.Version,
		},
		Height: 1,
	}, true, nil).WithGasMeter(storetypes.NewGasMeter(gasLimit)).WithIsCheckTx(true)

	// Build a tx with a MsgExec containing a MsgPayForBlobs with a huge gas cost
	txBuilder := txConfig.NewTxBuilder()
	msgExec := authz.NewMsgExec(sdk.AccAddress{}, []sdk.Msg{
		&blob.MsgPayForBlobs{
			Signer:    "celestia...",
			BlobSizes: []uint32{uint32(math.MaxUint32)},
		},
	})
	require.NoError(t, txBuilder.SetMsgs(&msgExec))
	tx := txBuilder.GetTx()

	// Run the ante handler
	_, err := anteHandler.AnteHandle(ctx, tx, false, mockNext)
	require.Error(t, err)
	require.ErrorIs(t, err, sdkerrors.ErrInsufficientFee)

	// Create a MsgExec that wraps a MsgExec that contains a MsgPayForBlobs with a huge gas cost
	nestedMsgExec := authz.NewMsgExec(sdk.AccAddress{}, []sdk.Msg{&msgExec})
	require.NoError(t, txBuilder.SetMsgs(&nestedMsgExec))
	tx = txBuilder.GetTx()

	// Run the ante handler
	_, err = anteHandler.AnteHandle(ctx, tx, false, mockNext)
	require.Error(t, err)
	require.ErrorIs(t, err, sdkerrors.ErrInsufficientFee)
}

type mockBlobKeeper struct{}

func (mockBlobKeeper) GetParams(sdk.Context) blob.Params {
	return blob.Params{
		GasPerBlobByte:   testGasPerBlobByte,
		GovMaxSquareSize: testGovMaxSquareSize,
	}
}
