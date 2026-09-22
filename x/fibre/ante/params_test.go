package ante

import (
	"testing"

	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestParamsVersionDecorator(t *testing.T) {
	for _, version := range []uint64{10, 11} {
		for _, params := range []fibretypes.Params{fibretypes.DefaultParamsForVersion(10), fibretypes.DefaultParams()} {
			ctx := sdk.Context{}.WithConsensusParams(cmtproto.ConsensusParams{Version: &cmtproto.VersionParams{App: version}})
			tx := mockTx{msgs: []sdk.Msg{&fibretypes.MsgUpdateFibreParams{Params: params}}}
			nextCalled := false
			_, err := (ParamsVersionDecorator{}).AnteHandle(ctx, tx, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { nextCalled = true; return ctx, nil })
			if version == 10 && params == fibretypes.DefaultParams() {
				require.Error(t, err)
				require.False(t, nextCalled)
			} else {
				require.NoError(t, err)
				require.True(t, nextCalled)
			}
		}
	}
}
