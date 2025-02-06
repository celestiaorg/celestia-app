package ante_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/ante"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"
)

func TestGovDecorator(t *testing.T) {
	decorator := ante.NewGovProposalDecorator()
	anteHandler := types.ChainAnteDecorators(decorator)
	accounts := testfactory.GenerateAccounts(1)
	coins := types.NewCoins(types.NewCoin(appconsts.BondDenom, types.NewInt(10)))

	msgSend := banktypes.NewMsgSend(
		testnode.RandomAddress().(types.AccAddress),
		testnode.RandomAddress().(types.AccAddress),
		coins,
	)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	msgProposal, err := govtypes.NewMsgSubmitProposal([]types.Msg{msgSend}, coins, accounts[0], "")
	require.NoError(t, err)
	msgEmptyProposal, err := govtypes.NewMsgSubmitProposal([]types.Msg{}, coins, accounts[0], "do nothing")
	require.NoError(t, err)

	testCases := []struct {
		name   string
		msg    []types.Msg
		expErr bool
	}{
		{
			name:   "good proposal; has at least one message",
			msg:    []types.Msg{msgProposal},
			expErr: false,
		},
		{
			name:   "bad proposal; has no messages",
			msg:    []types.Msg{msgEmptyProposal},
			expErr: true,
		},
		{
			name:   "good proposal; multiple messages in tx",
			msg:    []types.Msg{msgProposal, msgSend},
			expErr: false,
		},
		{
			name:   "bad proposal; multiple messages in tx but proposal has no messages",
			msg:    []types.Msg{msgEmptyProposal, msgSend},
			expErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := types.Context{}
			builder := encCfg.TxConfig.NewTxBuilder()
			require.NoError(t, builder.SetMsgs(tc.msg...))
			tx := builder.GetTx()
			_, err := anteHandler(ctx, tx, false)
			if tc.expErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
