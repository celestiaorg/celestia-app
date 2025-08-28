package ante_test

import (
	"errors"
	"testing"

	circuitante "cosmossdk.io/x/circuit/ante"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/test/util"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	ibcclienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	"github.com/stretchr/testify/require"
)

var terminalAnteHandler = func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
	return ctx, nil
}

func TestCircuitBreaker(t *testing.T) {
	testApp, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	circuitAnte := circuitante.NewCircuitBreakerDecorator(&testApp.CircuitKeeper)

	tests := []struct {
		name     string
		msg      sdk.Msg
		expError error
	}{
		{
			"success: bank send",
			&banktypes.MsgSend{},
			nil,
		},
		{
			"failure: software upgrade is blocked",
			&upgradetypes.MsgSoftwareUpgrade{},
			errors.New("tx type not allowed"),
		},
		{
			"failure: cancel upgrade is blocked",
			&upgradetypes.MsgCancelUpgrade{},
			errors.New("tx type not allowed"),
		},
		{
			"failure ibc software upgrade is blocked",
			&ibcclienttypes.MsgIBCSoftwareUpgrade{},
			errors.New("tx type not allowed"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			txBuilder := testApp.GetEncodingConfig().TxConfig.NewTxBuilder()
			err := txBuilder.SetMsgs(tc.msg)
			require.NoError(t, err)

			ctx := testApp.NewUncachedContext(true, cmtproto.Header{})
			newCtx, err := circuitAnte.AnteHandle(ctx, txBuilder.GetTx(), false, terminalAnteHandler)

			if tc.expError == nil {
				require.NoError(t, err)
				require.NotNil(t, newCtx)
			} else {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.expError.Error())
			}
		})
	}
}
