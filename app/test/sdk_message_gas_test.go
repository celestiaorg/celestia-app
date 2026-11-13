package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v10/test/util/testnode"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/celestiaorg/go-square/v4"
	abci "github.com/cometbft/cometbft/abci/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"
)

// TestSDKMessageGasAdmission checks that a tx whose gas limit does not cover
// 10k gas per counted SDK message beyond the first is kept out of the mempool
// and out of proposals this node builds, while blocks that include one stay
// valid.
func TestSDKMessageGasAdmission(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(7)
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

	// Enough gas to execute each attack, but less than the per-message floor.
	const underpaidGas = 1_000_000
	floor := uint64(appconsts.MaxSDKMessages-1) * 10_000

	s0, addr0 := signer(0)
	execAttack := createTx(s0, underpaidGas, emptyExecs(addr0)...)
	s1, addr1 := signer(1)
	icaAttack := createTx(s1, underpaidGas, junkICAPacket(addr1))
	s2, addr2 := signer(2)
	paidExec := createTx(s2, floor, emptyExecs(addr2)...)

	// Honest txs with many messages, each with a gas limit close to what it uses.
	s6, addr6 := signer(6)
	honestSends := make([]sdk.Msg, 20)
	for i := range honestSends {
		honestSends[i] = send(addr6)
	}
	honestExec := authz.NewMsgExec(addr6, honestSends)
	honestSendsTx := createTx(s6, 2_000_000, honestSends...)
	require.NoError(t, s6.IncrementSequence(accounts[6]))
	honestTxs := [][]byte{honestSendsTx, createTx(s6, 2_000_000, &honestExec)}

	sendTxs := make([][]byte, 3)
	for i := range sendTxs {
		s, addr := signer(3 + i)
		sendTxs[i] = createTx(s, 100_000, send(addr))
	}

	t.Run("CheckTx rejects an underpaid tx", func(t *testing.T) {
		for _, tx := range [][]byte{execAttack, icaAttack} {
			for _, typ := range []abci.CheckTxType{abci.CheckTxType_New, abci.CheckTxType_Recheck} {
				resp, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: tx, Type: typ})
				require.NoError(t, err)
				require.Equal(t, sdkerrors.ErrOutOfGas.ABCICode(), resp.Code, resp.Log)
				require.Equal(t, sdkerrors.ErrOutOfGas.Codespace(), resp.Codespace)
			}
		}
	})

	t.Run("CheckTx accepts a tx that pays the floor", func(t *testing.T) {
		resp, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: paidExec, Type: abci.CheckTxType_New})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, resp.Code, resp.Log)
	})

	t.Run("PrepareProposal keeps honest txs", func(t *testing.T) {
		resp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
			Txs:    append([][]byte{execAttack, icaAttack}, sendTxs...),
			Height: testApp.LastBlockHeight() + 1,
			Time:   time.Now(),
		})
		require.NoError(t, err)
		require.Equal(t, sendTxs, resp.Txs)
	})

	t.Run("ProcessProposal accepts an underpaid tx", func(t *testing.T) {
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
			require.Equal(t, abci.ResponseProcessProposal_ACCEPT, resp.Status)
		}
	})

	t.Run("honest txs use more gas than the floor", func(t *testing.T) {
		resp := commitBlock(t, testApp, honestTxs...)
		require.Len(t, resp.TxResults, len(honestTxs))
		weights := []int64{int64(len(honestSends)), int64(1 + len(honestSends))}
		for i, res := range resp.TxResults {
			require.Equal(t, abci.CodeTypeOK, res.Code, res.Log)
			require.GreaterOrEqual(t, res.GasUsed, (weights[i]-1)*10_000)
		}
	})
}
