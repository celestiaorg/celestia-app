package ante_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	v2 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v2"
	ante "github.com/celestiaorg/celestia-app/v3/x/blob/ante"
	blob "github.com/celestiaorg/celestia-app/v3/x/blob/types"
	"github.com/celestiaorg/go-square/v2/share"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/proto/tendermint/version"
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
		txGas       func(uint32) uint32
		gasConsumed uint64
		versions    []uint64
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
			versions:    []uint64{v2.Version, appconsts.LatestVersion},
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
			versions:    []uint64{v2.Version, appconsts.LatestVersion},
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
			versions:    []uint64{v2.Version, appconsts.LatestVersion},
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
			versions:    []uint64{v2.Version, appconsts.LatestVersion},
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
			versions:    []uint64{v2.Version, appconsts.LatestVersion},
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
			versions:    []uint64{v2.Version, appconsts.LatestVersion},
			wantErr:     false,
		},
	}
	for _, tc := range testCases {
		for _, currentVersion := range tc.versions {
			t.Run(fmt.Sprintf("%s v%d", tc.name, currentVersion), func(t *testing.T) {
				anteHandler := ante.NewMinGasPFBDecorator(mockBlobKeeper{})
				var gasPerBlobByte uint32
				if currentVersion == v2.Version {
					gasPerBlobByte = testGasPerBlobByte
				} else {
					gasPerBlobByte = appconsts.GasPerBlobByte(currentVersion)
				}

				ctx := sdk.NewContext(nil, tmproto.Header{
					Version: version.Consensus{
						App: currentVersion,
					},
				}, true, nil).WithGasMeter(sdk.NewGasMeter(uint64(tc.txGas(gasPerBlobByte)))).WithIsCheckTx(true)

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
}

// TestMinGasPFBDecoratorWithMsgExec tests that the MinGasPFBDecorator rejects a
// MsgExec containing a MsgPayForBlob with a gas cost greater than the tx's gas
// limit.
func TestMinGasPFBDecoratorWithMsgExec(t *testing.T) {
	anteHandler := ante.NewMinGasPFBDecorator(mockBlobKeeper{})
	txConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...).TxConfig

	// Create a context with a gas meter with a high gas limit
	gasLimit := uint64(10000)
	ctx := sdk.NewContext(nil, tmproto.Header{
		Version: version.Consensus{
			App: appconsts.LatestVersion,
		},
	}, true, nil).WithGasMeter(sdk.NewGasMeter(gasLimit)).WithIsCheckTx(true)

	// Build a tx with a MsgExec containing a MsgPayForBlobs with a huge gas cost
	txBuilder := txConfig.NewTxBuilder()
	nestedPFB := authz.NewMsgExec(sdk.AccAddress{}, []sdk.Msg{
		&blob.MsgPayForBlobs{
			Signer:    "celestia...",
			BlobSizes: []uint32{uint32(math.MaxUint32)},
		},
	})

	require.NoError(t, txBuilder.SetMsgs(&nestedPFB))
	tx := txBuilder.GetTx()

	// Run the ante handler
	_, err := anteHandler.AnteHandle(ctx, tx, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil })
	require.Error(t, err)
	require.ErrorIs(t, err, sdkerrors.ErrInsufficientFee)
}

type mockBlobKeeper struct{}

func (mockBlobKeeper) GasPerBlobByte(_ sdk.Context) uint32 {
	return testGasPerBlobByte
}

func (mockBlobKeeper) GovMaxSquareSize(_ sdk.Context) uint64 {
	return testGovMaxSquareSize
}
