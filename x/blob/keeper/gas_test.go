package keeper_test

import (
	"testing"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/x/blob/types"
	"github.com/celestiaorg/go-square/v2/share"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestPayForBlobGas(t *testing.T) {
	type testCase struct {
		name            string
		msg             types.MsgPayForBlobs
		wantGasConsumed uint64
	}

	testCases := []testCase{
		{
			name:            "1 byte blob", // occupies 1 share
			msg:             types.MsgPayForBlobs{BlobSizes: []uint32{1}},
			wantGasConsumed: uint64(1 * share.ShareSize * appconsts.GasPerBlobByte(appconsts.LatestVersion)), // 1 share * 512 bytes per share * 8 gas per byte= 4096 gas
		},
		{
			name:            "100 byte blob", // occupies 1 share
			msg:             types.MsgPayForBlobs{BlobSizes: []uint32{100}},
			wantGasConsumed: uint64(1 * share.ShareSize * appconsts.GasPerBlobByte(appconsts.LatestVersion)),
		},
		{
			name:            "1024 byte blob", // occupies 3 shares because share prefix (e.g. namespace, info byte)
			msg:             types.MsgPayForBlobs{BlobSizes: []uint32{1024}},
			wantGasConsumed: uint64(3 * share.ShareSize * appconsts.GasPerBlobByte(appconsts.LatestVersion)), // 3 shares * 512 bytes per share * 8 gas per byte = 12288 gas
		},
		{
			name:            "3 blobs, 1 share each",
			msg:             types.MsgPayForBlobs{BlobSizes: []uint32{1, 1, 1}},
			wantGasConsumed: uint64(3 * share.ShareSize * appconsts.GasPerBlobByte(appconsts.LatestVersion)), // 3 shares * 512 bytes per share * 8 gas per byte = 12288 gas
		},
		{
			name:            "3 blobs, 6 shares total",
			msg:             types.MsgPayForBlobs{BlobSizes: []uint32{1024, 800, 100}},
			wantGasConsumed: uint64(6 * share.ShareSize * appconsts.GasPerBlobByte(appconsts.LatestVersion)), // 6 shares * 512 bytes per share * 8 gas per byte = 24576 gas
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			k, stateStore, _ := CreateKeeper(t, appconsts.LatestVersion)
			ctx := sdk.NewContext(stateStore, tmproto.Header{}, false, log.NewNopLogger())
			_, err := k.PayForBlobs(ctx, &tc.msg)
			require.NoError(t, err)
			if tc.wantGasConsumed != ctx.GasMeter().GasConsumed() {
				t.Errorf("Gas consumed by %s: %d, want: %d", tc.name, ctx.GasMeter().GasConsumed(), tc.wantGasConsumed)
			}
		})
	}
}

func TestChangingGasParam(t *testing.T) {
	// TODO: can we just remove this test now... are we using params in x/blob for gas costs or is it just versioned params using appconsts??
	// Test errors because k.PayForBlobs uses a constant gas price from appconsts now rather than params.
	t.Skip("skipping x/blob gas param change test - x/blob keeper is using appconsts gas value")

	msg := types.MsgPayForBlobs{BlobSizes: []uint32{1024}}
	k, stateStore, _ := CreateKeeper(t, appconsts.LatestVersion)
	tempCtx := sdk.NewContext(stateStore, tmproto.Header{}, false, log.NewNopLogger())

	ctx1 := sdk.NewContext(stateStore, tmproto.Header{}, false, log.NewNopLogger())
	_, err := k.PayForBlobs(ctx1, &msg)
	require.NoError(t, err)

	params := k.GetParams(tempCtx)
	params.GasPerBlobByte++
	k.SetParams(tempCtx, params)

	ctx2 := sdk.NewContext(stateStore, tmproto.Header{}, false, log.NewNopLogger())
	_, err = k.PayForBlobs(ctx2, &msg)
	require.NoError(t, err)

	if ctx1.GasMeter().GasConsumed() >= ctx2.GasMeter().GasConsumedToLimit() {
		t.Errorf("Gas consumed was not increased upon incrementing param, before: %d, after: %d",
			ctx1.GasMeter().GasConsumed(), ctx2.GasMeter().GasConsumedToLimit(),
		)
	}
}
