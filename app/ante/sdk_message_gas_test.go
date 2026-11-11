package ante_test

import (
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/ante"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

func TestSDKMessageGasDecorator(t *testing.T) {
	rate := appconsts.GasPerAdditionalSDKMessage

	tests := []struct {
		name    string
		weight  int
		wantGas uint64
	}{
		{name: "one message is free", weight: 1, wantGas: 0},
		{name: "two messages", weight: 2, wantGas: rate},
		{name: "a full block of messages", weight: appconsts.MaxSDKMessages, wantGas: uint64(appconsts.MaxSDKMessages-1) * rate},
	}

	cdc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// The decorator charges for the weight the counter reports, not
			// for the number of top-level messages.
			decorator := ante.NewSDKMessageGasDecorator(func(sdk.Context, []sdk.Msg) int { return tc.weight })
			txBuilder := cdc.TxConfig.NewTxBuilder()
			require.NoError(t, txBuilder.SetMsgs(&banktypes.MsgSend{}))
			ctx := sdk.Context{}.WithGasMeter(storetypes.NewInfiniteGasMeter())
			nextCalled := false

			// The charge applies in simulation too, so gas estimates cover it.
			gotCtx, err := decorator.AnteHandle(ctx, txBuilder.GetTx(), true, func(ctx sdk.Context, _ sdk.Tx, simulate bool) (sdk.Context, error) {
				nextCalled = true
				require.True(t, simulate)
				return ctx, nil
			})

			require.NoError(t, err)
			require.True(t, nextCalled)
			require.Equal(t, tc.wantGas, gotCtx.GasMeter().GasConsumed())
		})
	}
}
