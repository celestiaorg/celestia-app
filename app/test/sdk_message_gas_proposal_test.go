package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v11/test/util/testnode"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/celestiaorg/go-square/v4"
	abci "github.com/cometbft/cometbft/abci/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"
)

// TestProposalChargesGasPerSDKMessage checks that a tx whose gas limit does not
// cover GasPerAdditionalSDKMessage per counted SDK message beyond the first
// cannot take the block's SDK message budget: PrepareProposal drops it and
// keeps the honest txs, and ProcessProposal rejects a block that includes it.
func TestProposalChargesGasPerSDKMessage(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(5)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	seedICAHostChannel(t, testApp)
	infos := queryAccountInfo(testApp, accounts, kr)

	signer := func(i int) (*user.Signer, sdk.AccAddress) {
		s, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, user.NewAccount(accounts[i], infos[i].AccountNum, infos[i].Sequence))
		require.NoError(t, err)
		return s, testfactory.GetAddress(kr, accounts[i])
	}
	createTx := func(s *user.Signer, gasLimit uint64, msgs ...sdk.Msg) []byte {
		tx, _, err := s.CreateTx(msgs, user.SetGasLimitAndGasPrice(gasLimit, appconsts.DefaultMinGasPrice))
		require.NoError(t, err)
		return tx
	}
	send := func(from sdk.AccAddress) sdk.Msg {
		return banktypes.NewMsgSend(from, testnode.RandomAddress().(sdk.AccAddress), sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 10)))
	}
	// Six MsgExec with 99 empty MsgSends each weigh MaxSDKMessages.
	emptyExecs := func(addr sdk.AccAddress) []sdk.Msg {
		msgs := make([]sdk.Msg, 6)
		for i := range msgs {
			inner := make([]sdk.Msg, 99)
			for j := range inner {
				inner[j] = &banktypes.MsgSend{}
			}
			exec := authz.NewMsgExec(addr, inner)
			msgs[i] = &exec
		}
		return msgs
	}
	// An ICA packet with MaxSDKMessages-1 junk entries weighs MaxSDKMessages.
	junkICAPacket := func(addr sdk.AccAddress) sdk.Msg {
		entries := make([]*codectypes.Any, appconsts.MaxSDKMessages-1)
		for i := range entries {
			entries[i] = &codectypes.Any{TypeUrl: "x"}
		}
		payload, err := enc.Codec.Marshal(&icatypes.CosmosTx{Messages: entries})
		require.NoError(t, err)
		data := icatypes.InterchainAccountPacketData{Type: icatypes.EXECUTE_TX, Data: payload}
		packet := channeltypes.NewPacket(data.GetBytes(), 1, "icacontroller-test", "channel-0", icatypes.HostPortID, icaChannelID, clienttypes.NewHeight(1, 1000000), 0)
		return channeltypes.NewMsgRecvPacket(packet, []byte("proof"), clienttypes.NewHeight(1, 1), addr.String())
	}

	// Enough gas to execute each attack, but not the per-message charge.
	const underpaidGas = 1_000_000

	s0, addr0 := signer(0)
	execAttack := createTx(s0, underpaidGas, emptyExecs(addr0)...)
	s1, addr1 := signer(1)
	icaAttack := createTx(s1, underpaidGas, junkICAPacket(addr1))

	sendTxs := make([][]byte, 3)
	for i := range sendTxs {
		s, addr := signer(2 + i)
		sendTxs[i] = createTx(s, 100_000, send(addr))
	}

	t.Run("PrepareProposal keeps honest txs", func(t *testing.T) {
		resp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
			Txs:    append([][]byte{execAttack, icaAttack}, sendTxs...),
			Height: testApp.LastBlockHeight() + 1,
			Time:   time.Now(),
		})
		require.NoError(t, err)
		require.Equal(t, sendTxs, resp.Txs)
	})

	t.Run("ProcessProposal rejects an underpaid tx", func(t *testing.T) {
		for _, tx := range [][]byte{execAttack, icaAttack} {
			blockTxs := [][]byte{tx}
			classifiedTxs, err := fibretypes.ClassifyTxs(blockTxs)
			require.NoError(t, err)
			dataSquare, err := square.Construct(classifiedTxs, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
			require.NoError(t, err)
			squareSize, err := dataSquare.Size()
			require.NoError(t, err)

			resp, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
				Height:       testApp.LastBlockHeight() + 1,
				Time:         time.Now(),
				Txs:          blockTxs,
				DataRootHash: calculateNewDataHash(t, blockTxs),
				SquareSize:   uint64(squareSize),
			})
			require.NoError(t, err)
			require.Equal(t, abci.ResponseProcessProposal_REJECT, resp.Status)
		}
	})
}
