package keeper_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	v3 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v3"
	"github.com/celestiaorg/celestia-app/v3/x/blob/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestPayForBlobGas(t *testing.T) {
	type testCase struct {
		name            string
		msg             types.MsgPayForBlobs
		wantGasConsumed uint64
	}

	paramLookUpCost := uint32(1060)

	testCases := []testCase{
		{
			name:            "1 byte blob", // occupies 1 share
			msg:             types.MsgPayForBlobs{BlobSizes: []uint32{1}},
			wantGasConsumed: uint64(1*appconsts.ShareSize*appconsts.GasPerBlobByte(v3.Version) + paramLookUpCost), // 1 share * 512 bytes per share * 8 gas per byte + 1060 gas for fetching param = 5156 gas
		},
		{
			name:            "100 byte blob", // occupies 1 share
			msg:             types.MsgPayForBlobs{BlobSizes: []uint32{100}},
			wantGasConsumed: uint64(1*appconsts.ShareSize*appconsts.GasPerBlobByte(v3.Version) + paramLookUpCost),
		},
		{
			name:            "1024 byte blob", // occupies 3 shares because share prefix (e.g. namespace, info byte)
			msg:             types.MsgPayForBlobs{BlobSizes: []uint32{1024}},
			wantGasConsumed: uint64(3*appconsts.ShareSize*appconsts.GasPerBlobByte(v3.Version) + paramLookUpCost), // 3 shares * 512 bytes per share * 8 gas per byte + 1060 gas for fetching param = 13348 gas
		},
		{
			name:            "3 blobs, 1 share each",
			msg:             types.MsgPayForBlobs{BlobSizes: []uint32{1, 1, 1}},
			wantGasConsumed: uint64(3*appconsts.ShareSize*appconsts.GasPerBlobByte(v3.Version) + paramLookUpCost), // 3 shares * 512 bytes per share * 8 gas per byte + 1060 gas for fetching param = 13348 gas
		},
		{
			name:            "3 blobs, 6 shares total",
			msg:             types.MsgPayForBlobs{BlobSizes: []uint32{1024, 800, 100}},
			wantGasConsumed: uint64(6*appconsts.ShareSize*appconsts.GasPerBlobByte(v3.Version) + paramLookUpCost), // 6 shares * 512 bytes per share * 8 gas per byte + 1060 gas for fetching param = 25636 gas
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			k, stateStore, _ := CreateKeeper(t, appconsts.LatestVersion)
			ctx := sdk.NewContext(stateStore, tmproto.Header{}, false, nil)
			_, err := k.PayForBlobs(sdk.WrapSDKContext(ctx), &tc.msg)
			require.NoError(t, err)
			if tc.wantGasConsumed != ctx.GasMeter().GasConsumed() {
				t.Errorf("Gas consumed by %s: %d, want: %d", tc.name, ctx.GasMeter().GasConsumed(), tc.wantGasConsumed)
			}
		})
	}
}

func TestChangingGasParam(t *testing.T) {
	msg := types.MsgPayForBlobs{BlobSizes: []uint32{1024}}
	k, stateStore, _ := CreateKeeper(t, appconsts.LatestVersion)
	tempCtx := sdk.NewContext(stateStore, tmproto.Header{}, false, nil)

	ctx1 := sdk.NewContext(stateStore, tmproto.Header{}, false, nil)
	_, err := k.PayForBlobs(sdk.WrapSDKContext(ctx1), &msg)
	require.NoError(t, err)

	params := k.GetParams(tempCtx)
	params.GasPerBlobByte++
	k.SetParams(tempCtx, params)

	ctx2 := sdk.NewContext(stateStore, tmproto.Header{}, false, nil)
	_, err = k.PayForBlobs(sdk.WrapSDKContext(ctx2), &msg)
	require.NoError(t, err)

	if ctx1.GasMeter().GasConsumed() >= ctx2.GasMeter().GasConsumedToLimit() {
		t.Errorf("Gas consumed was not increased upon incrementing param, before: %d, after: %d",
			ctx1.GasMeter().GasConsumed(), ctx2.GasMeter().GasConsumedToLimit(),
		)
	}
}
