package ante_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/ante"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
)

func TestGovDecorator(t *testing.T) {
	blockedParams := map[string][]string{
		gogoproto.MessageName(&banktypes.MsgUpdateParams{}):      {"send_enabled"},
		gogoproto.MessageName(&stakingtypes.MsgUpdateParams{}):   {"params.bond_denom", "params.unbonding_time"},
		gogoproto.MessageName(&consensustypes.MsgUpdateParams{}): {"validator"},
	}

	decorator := ante.NewGovProposalDecorator(blockedParams)
	anteHandler := types.ChainAnteDecorators(decorator)
	accountStr := testnode.RandomAddress().String()
	coins := types.NewCoins(types.NewCoin(appconsts.BondDenom, math.NewInt(10)))

	enc := encoding.MakeTestConfig(app.ModuleEncodingRegisters...)
	from := testnode.RandomAddress().Bytes()
	to := testnode.RandomAddress().Bytes()
	msgSend := banktypes.NewMsgSend(from, to, coins)

	msgProposal, err := govtypes.NewMsgSubmitProposal(
		[]types.Msg{msgSend}, coins, accountStr, "", "", "", false)
	require.NoError(t, err)
	msgEmptyProposal, err := govtypes.NewMsgSubmitProposal(
		[]types.Msg{}, coins, accountStr, "do nothing", "", "", false)
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
			builder := enc.TxConfig.NewTxBuilder()
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
