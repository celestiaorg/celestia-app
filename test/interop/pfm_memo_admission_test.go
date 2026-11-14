package interop

import (
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/da"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	square "github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/share"
	abci "github.com/cometbft/cometbft/abci/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	host "github.com/cosmos/ibc-go/v8/modules/core/24-host"
	ibctesting "github.com/cosmos/ibc-go/v8/testing"
	"github.com/stretchr/testify/require"
)

// TestPacketForwardMemoAdmission checks that packets with forward memos, and
// gov proposals, that may execute differently across builds are kept out of
// the mempool and out of proposals this node builds, that honest forward memos
// are not, and that blocks including the rejected txs stay valid.
func TestPacketForwardMemoAdmission(t *testing.T) {
	coordinator, celestia, chainA, chainB := SetupTest(t)
	pathA := ibctesting.NewTransferPath(chainA, celestia)
	pathB := ibctesting.NewTransferPath(celestia, chainB)
	coordinator.Setup(pathA)
	coordinator.Setup(pathB)
	celestiaApp := celestia.App.(*app.App)

	expand := func(memo string) string {
		memo = strings.ReplaceAll(memo, "{CH}", pathB.EndpointA.ChannelID)
		return strings.ReplaceAll(memo, "{RCV}", chainB.SenderAccount.GetAddress().String())
	}
	honestMemos := []string{
		`{"forward":{"receiver":"{RCV}","port":"transfer","channel":"{CH}"}}`,
		`{"forward":{"receiver":"{RCV}","port":"transfer","channel":"{CH}","timeout":"10m","retries":2}}`,
		`{"forward":{"receiver":"{RCV}","port":"transfer","channel":"{CH}","timeout":600000000000}}`,
		`{"forward":{"receiver":"{RCV}","port":"transfer","channel":"{CH}","next":{"forward":{"receiver":"osmo1r","port":"transfer","channel":"channel-2"}}}}`,
		`{"forward":{"receiver":"{RCV}","port":"transfer","channel":"{CH}","next":"{\"forward\":{\"receiver\":\"osmo1r\",\"port\":\"transfer\",\"channel\":\"channel-2\"}}"}}`,
		`{"forward":{"receiver":"{RCV}","port":"transfer","channel":"{CH}","retries":null,"next":null}}`,
		`{"forward":{"receiver":"{RCV}","port":"transfer","channel":"{CH}","next":{"memo":"😀"}}}`,
		`{"Forward":{"receiver":"x","port":"a/b","channel":"{CH}"}}`,
		// Nesting depth at the limit of 16.
		`{"forward":{"receiver":"{RCV}","port":"transfer","channel":"{CH}","next":` + nestedObject(14) + `}}`,
	}
	badMemos := []string{
		`{"forward":{"receiver":"x","port":"a/b","channel":"{CH}"}}`,
		`{"forward":{"receiver":"x","port":"transfer","channel":"channel/1"}}`,
		`{"forward":{"receiver":5,"port":"transfer","channel":"{CH}"}}`,
		`{"forward":{"receiver":"x","port":true,"channel":"{CH}"}}`,
		`{"forward":{"receiver":"x","port":"transfer","channel":"{CH}","retries":300}}`,
		`{"forward":{"receiver":"{RCV}","port":"transfer","channel":"{CH}","timeout":1e19}}`,
		`{"forward":{"receiver":"{RCV}","port":"transfer","channel":"{CH}","timeout":-1e19}}`,
		// The packet-forward memo limits of main.
		`{"forward":{"receiver":"{RCV}","port":"transfer","channel":"{CH}","next":` + nestedObject(15) + `}}`,
		`{"forward":{"receiver":"{RCV}","port":"transfer","channel":"{CH}","next":{"a":1},"next":{}}}`,
		`{"forward":{"receiver":"{RCV}","port":"transfer","channel":"{CH}","next":{"a":1,"a":2}}}`,
		`{"forward":{"receiver":"{RCV}","port":"transfer","channel":"{CH}","next":"null"}}`,
		`{"forward":{"receiver":"{RCV}","port":"transfer","channel":"{CH}","next":[{"a":1}]}}`,
		`{"forward":{"receiver":"{RCV}","port":"transfer","channel":"{CH}","timeout":"1ns","retries":255,"next":` + strconv.Quote(nestedObject(3000)) + `}}`,
	}
	execMemo := `{"forward":{"receiver":"{RCV}","port":"transfer","channel":"{CH}","timeout":9.3e18}}`

	// Send every packet from chainA, then update Celestia's client once so all
	// proofs verify.
	sendPacket := func(memo string) channeltypes.Packet {
		msg := transfertypes.NewMsgTransfer(pathA.EndpointA.ChannelConfig.PortID, pathA.EndpointA.ChannelID,
			sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(1000)), chainA.SenderAccount.GetAddress().String(),
			"celestia-receiver", clienttypes.NewHeight(1, 100000), 0, expand(memo))
		res, err := chainA.SendMsgs(msg)
		require.NoError(t, err)
		packet, err := ibctesting.ParsePacketFromEvents(res.GetEvents())
		require.NoError(t, err)
		return packet
	}
	honestPackets := make([]channeltypes.Packet, len(honestMemos))
	for i, memo := range honestMemos {
		honestPackets[i] = sendPacket(memo)
	}
	badPackets := make([]channeltypes.Packet, len(badMemos))
	for i, memo := range badMemos {
		badPackets[i] = sendPacket(memo)
	}
	execPacket := sendPacket(execMemo)
	require.NoError(t, pathA.EndpointB.UpdateClient())

	recvMsg := func(packet channeltypes.Packet, signer sdk.AccAddress) *channeltypes.MsgRecvPacket {
		proof, proofHeight := chainA.QueryProof(host.PacketCommitmentKey(packet.SourcePort, packet.SourceChannel, packet.Sequence))
		return channeltypes.NewMsgRecvPacket(packet, proof, proofHeight, signer.String())
	}
	// signTxs signs one tx per msg list from sender account i, with consecutive sequences.
	signTxs := func(i int, msgLists ...[]sdk.Msg) [][]byte {
		acc := celestiaApp.AccountKeeper.GetAccount(celestia.GetContext(), celestia.SenderAccounts[i].SenderAccount.GetAddress())
		txs := make([][]byte, len(msgLists))
		for j, msgs := range msgLists {
			tx, err := simtestutil.GenSignedMockTx(rand.New(rand.NewSource(int64(j))), celestia.TxConfig, msgs,
				sdk.Coins{sdk.NewInt64Coin(sdk.DefaultBondDenom, 40_000)}, simtestutil.DefaultGenTxGas, celestia.ChainID,
				[]uint64{acc.GetAccountNumber()}, []uint64{acc.GetSequence() + uint64(j)}, celestia.SenderAccounts[i].SenderPrivKey)
			require.NoError(t, err)
			txs[j], err = celestia.TxConfig.TxEncoder()(tx)
			require.NoError(t, err)
		}
		return txs
	}

	honestSigner := celestia.SenderAccounts[1].SenderAccount.GetAddress()
	honestMsgs := make([][]sdk.Msg, len(honestPackets))
	for i, packet := range honestPackets {
		honestMsgs[i] = []sdk.Msg{recvMsg(packet, honestSigner)}
	}
	honestTxs := signTxs(1, honestMsgs...)

	badSigner := celestia.SenderAccounts[2].SenderAccount.GetAddress()
	badMsgs := make([][]sdk.Msg, 0, len(badPackets)+1)
	for _, packet := range badPackets {
		badMsgs = append(badMsgs, []sdk.Msg{recvMsg(packet, badSigner)})
	}
	exec := authz.NewMsgExec(badSigner, []sdk.Msg{recvMsg(execPacket, badSigner)})
	badMsgs = append(badMsgs, []sdk.Msg{&exec})
	govParams := govv1.DefaultParams()
	zero := time.Duration(0)
	govParams.MaxDepositPeriod = &zero
	govProposal := func(proposer sdk.AccAddress) sdk.Msg {
		msg, err := govv1.NewMsgSubmitProposal([]sdk.Msg{&govv1.MsgUpdateParams{Authority: authtypes.NewModuleAddress(govtypes.ModuleName).String(), Params: govParams}},
			sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 100_000_000)), proposer.String(), "", "title", "summary", false)
		require.NoError(t, err)
		return msg
	}
	badMsgs = append(badMsgs, []sdk.Msg{govProposal(badSigner)})
	badTxs := signTxs(2, badMsgs...)

	t.Run("CheckTx rejects divergent forward memos", func(t *testing.T) {
		for i, tx := range badTxs {
			for _, typ := range []abci.CheckTxType{abci.CheckTxType_New, abci.CheckTxType_Recheck} {
				resp, err := celestiaApp.CheckTx(&abci.RequestCheckTx{Tx: tx, Type: typ})
				require.NoError(t, err)
				require.Equal(t, sdkerrors.ErrInvalidRequest.ABCICode(), resp.Code, "tx %d: %s", i, resp.Log)
				require.Equal(t, sdkerrors.ErrInvalidRequest.Codespace(), resp.Codespace)
				require.Regexp(t, "unsupported packet-forward memo|gov max_deposit_period must be positive", resp.Log)
			}
		}
	})

	t.Run("CheckTx accepts honest forward memos", func(t *testing.T) {
		for i, tx := range honestTxs {
			resp, err := celestiaApp.CheckTx(&abci.RequestCheckTx{Tx: tx, Type: abci.CheckTxType_New})
			require.NoError(t, err)
			require.Equal(t, abci.CodeTypeOK, resp.Code, "tx %d: %s", i, resp.Log)
		}
	})

	t.Run("PrepareProposal drops divergent forward memos", func(t *testing.T) {
		resp, err := celestiaApp.PrepareProposal(&abci.RequestPrepareProposal{
			Txs:    append(append([][]byte{}, badTxs...), honestTxs...),
			Height: celestiaApp.LastBlockHeight() + 1,
			Time:   celestia.CurrentHeader.Time,
		})
		require.NoError(t, err)
		require.Equal(t, honestTxs, resp.Txs)
	})

	t.Run("ProcessProposal accepts divergent forward memos", func(t *testing.T) {
		blockTxs := append(append([][]byte{}, honestTxs...), badTxs...)
		classifiedTxs, err := fibretypes.ClassifyTxs(blockTxs)
		require.NoError(t, err)
		dataSquare, err := square.Construct(classifiedTxs, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
		require.NoError(t, err)
		squareSize, err := dataSquare.Size()
		require.NoError(t, err)
		eds, err := da.ExtendShares(share.ToBytes(dataSquare))
		require.NoError(t, err)
		dah, err := da.NewDataAvailabilityHeader(eds)
		require.NoError(t, err)

		resp, err := celestiaApp.ProcessProposal(&abci.RequestProcessProposal{
			Height:       celestiaApp.LastBlockHeight() + 1,
			Time:         celestia.CurrentHeader.Time,
			Txs:          blockTxs,
			DataRootHash: dah.Hash(),
			SquareSize:   uint64(squareSize),
		})
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_ACCEPT, resp.Status)
	})

	t.Run("FinalizeBlock executes divergent forward memos", func(t *testing.T) {
		signer := celestia.SenderAccount.GetAddress()
		msgs := make([]sdk.Msg, 0, len(badPackets)+2)
		for _, packet := range badPackets {
			msgs = append(msgs, recvMsg(packet, signer))
		}
		exec := authz.NewMsgExec(signer, []sdk.Msg{recvMsg(execPacket, signer)})
		msgs = append(msgs, &exec, govProposal(signer))
		for _, msg := range msgs {
			res, err := celestia.SendMsgs(msg)
			require.NoError(t, err)
			require.Equal(t, abci.CodeTypeOK, res.Code, res.Log)
		}
	})
}

// nestedObject returns a JSON object that nests depth levels.
func nestedObject(depth int) string {
	return strings.Repeat(`{"a":`, depth-1) + `{}` + strings.Repeat(`}`, depth-1)
}
