package keeper

import (
	"testing"

	"github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestPayForBlobGas(t *testing.T) {
	type testCase struct {
		name            string
		msg             types.MsgPayForBlob
		wantGasConsumed uint64
	}

	testCases := []testCase{
		{
			name:            "1 byte blob", // occupies 1 share
			msg:             types.MsgPayForBlob{BlobSize: 1},
			wantGasConsumed: uint64(4096), // 1 share * 512 bytes per share * 8 gas per byte = 4096 gas
		},
		{
			name:            "100 byte blob", // occupies 1 share
			msg:             types.MsgPayForBlob{BlobSize: 100},
			wantGasConsumed: uint64(4096),
		},
		{
			name:            "1024 byte blob", // occupies 3 shares because share prefix (e.g. namespace, info byte)
			msg:             types.MsgPayForBlob{BlobSize: 1024},
			wantGasConsumed: uint64(12288), // 3 shares * 512 bytes per share * 8 gas per byte = 12288 gas
		},
	}

	app := simapp.Setup(t, false)
	for _, tc := range testCases {
		ctx := app.BaseApp.NewContext(false, tmproto.Header{})
		k := Keeper{}
		_, err := k.PayForBlob(sdk.WrapSDKContext(ctx), &tc.msg)
		require.NoError(t, err)
		if tc.wantGasConsumed != ctx.GasMeter().GasConsumed() {
			t.Errorf("Gas consumed by %s: %d, want: %d", tc.name, ctx.GasMeter().GasConsumed(), tc.wantGasConsumed)
		}
	}
}

func TestChangingGasParam(t *testing.T) {
	app := simapp.Setup(t, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	k := Keeper{}

	msg := types.MsgPayForBlob{BlobSize: 1024}
	_, err := k.PayForBlob(sdk.WrapSDKContext(ctx), &msg)
	require.NoError(t, err)

	tempCtx := app.BaseApp.NewContext(false, tmproto.Header{})
	params := k.GetParams(tempCtx)
	params.GasPerBlobByte++
	k.SetParams(ctx, params)

	ctx2 := app.BaseApp.NewContext(false, tmproto.Header{})
	_, err = k.PayForBlob(sdk.WrapSDKContext(ctx2), &msg)
	require.NoError(t, err)

	if ctx.GasMeter().GasConsumed() >= ctx2.GasMeter().GasConsumedToLimit() {
		t.Errorf("Gas consumed was not increased upon incrementing param, before: %d, after: %d",
			ctx.GasMeter().GasConsumed(), ctx2.GasMeter().GasConsumedToLimit(),
		)
	}
}
