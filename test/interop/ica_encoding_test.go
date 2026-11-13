package interop

import (
	"math/rand"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app"
	apperr "github.com/celestiaorg/celestia-app/v11/app/errors"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/da"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/share"
	abci "github.com/cometbft/cometbft/abci/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	host "github.com/cosmos/ibc-go/v8/modules/core/24-host"
	ibctesting "github.com/cosmos/ibc-go/v8/testing"
	"github.com/stretchr/testify/require"
)

// jsonTxWithNoProtoEntries returns a proto3json CosmosTx whose bytes also walk
// as a proto3 stream with no message entries. Every JSON token sits inside a
// proto field other than a message entry: a short token follows a '\t' (field
// 1, fixed64), and a long string token is covered by its own opening quote ('"'
// is a field 4 length prefix).
func jsonTxWithNoProtoEntries(tokens []string) []byte {
	var out []byte
	next := 0 // offset of the next proto tag
	pad := func() {
		for len(out) < next {
			out = append(out, ' ')
		}
	}
	for _, tok := range tokens {
		pad()
		if len(tok) <= 8 {
			out = append(out, '\t')
			next = len(out) + 8
			out = append(out, tok...)
			continue
		}
		if tok[0] != '"' || int(tok[1]) < len(tok)-2 {
			panic("token does not fit a proto field: " + tok)
		}
		start := len(out)
		out = append(out, tok...)
		next = start + 2 + int(tok[1])
	}
	pad()
	return out
}

// jsonSends returns a proto3json CosmosTx of n bank sends laid out by
// jsonTxWithNoProtoEntries.
func jsonSends(from, to, denom string, n int) []byte {
	q := func(s string) string { return `"` + s + `"` }
	tokens := []string{"{", q("messages"), ":", "["}
	for i := range n {
		if i > 0 {
			tokens = append(tokens, ",")
		}
		tokens = append(tokens, "{", q("@type"), ":", q("/cosmos.bank.v1beta1.MsgSend"), ",",
			q("from_address"), ":", q(from), ",", q("to_address"), ":", q(to), ",",
			q("amount"), ":", "[", "{", q("denom"), ":", q(denom), ",", q("amount"), ":", q("1"), "}", "]", "}")
	}
	tokens = append(tokens, "]", "}")
	return jsonTxWithNoProtoEntries(tokens)
}

// TestICAPacketCountIsEncodingIndependent checks that the messages of an ICA
// packet count against MaxSDKMessages whatever encoding the channel has when
// the tx is counted.
func TestICAPacketCountIsEncodingIndependent(t *testing.T) {
	coordinator, celestia, sim, _ := SetupTest(t)
	capp := celestia.App.(*app.App)
	sapp := sim.App.(*SimApp)

	path := ibctesting.NewPath(celestia, sim)
	coordinator.SetupConnections(path)
	hostEnd, ctrl := path.EndpointA, path.EndpointB

	// The counterparty's IBC state is written directly, so the test controls
	// each handshake and upgrade step on that side.
	ctrlPort, ctrlChan := "icacontroller-test", "channel-100"
	ctrl.ChannelConfig.PortID, ctrl.ChannelID = ctrlPort, ctrlChan
	ctrl.ChannelConfig.Order = channeltypes.ORDERED
	hostEnd.ChannelConfig.PortID = icatypes.HostPortID
	hostEnd.ChannelConfig.Order = channeltypes.ORDERED

	md := icatypes.NewMetadata(icatypes.Version, ctrl.ConnectionID, hostEnd.ConnectionID, "", icatypes.EncodingProtobuf, icatypes.TxTypeSDKMultiMsg)
	v1 := string(icatypes.ModuleCdc.MustMarshalJSON(&md))
	ctrl.ChannelConfig.Version, hostEnd.ChannelConfig.Version = v1, v1

	// 1. Counterparty ChanOpenInit; Celestia ChanOpenTry.
	sapp.IBCKeeper.ChannelKeeper.SetChannel(sim.GetContext(), ctrlPort, ctrlChan, channeltypes.NewChannel(
		channeltypes.INIT, channeltypes.ORDERED, channeltypes.NewCounterparty(icatypes.HostPortID, ""), []string{ctrl.ConnectionID}, v1))
	coordinator.CommitBlock(sim)
	require.NoError(t, hostEnd.ChanOpenTry())
	hostChan := hostEnd.ChannelID
	v1addr := hostEnd.GetChannel().Version

	// 2. Counterparty ChanOpenAck; Celestia ChanOpenConfirm.
	sapp.IBCKeeper.ChannelKeeper.SetChannel(sim.GetContext(), ctrlPort, ctrlChan, channeltypes.NewChannel(
		channeltypes.OPEN, channeltypes.ORDERED, channeltypes.NewCounterparty(icatypes.HostPortID, hostChan), []string{ctrl.ConnectionID}, v1addr))
	coordinator.CommitBlock(sim)
	require.NoError(t, hostEnd.ChanOpenConfirm())
	require.Equal(t, channeltypes.OPEN, hostEnd.GetChannel().State)

	mdAddr, err := icatypes.MetadataFromVersion(v1addr)
	require.NoError(t, err)
	icaAddr := mdAddr.Address
	denom := sdk.DefaultBondDenom
	const nInner = 2000

	// Fund the interchain account so every inner send would succeed.
	icaAcc, err := sdk.AccAddressFromBech32(icaAddr)
	require.NoError(t, err)
	_, err = celestia.SendMsgs(banktypes.NewMsgSend(celestia.SenderAccount.GetAddress(), icaAcc, sdk.NewCoins(sdk.NewInt64Coin(denom, 10*nInner))))
	require.NoError(t, err)

	// 3. Counterparty ChanUpgradeInit to proto3json; Celestia ChanUpgradeTry.
	md2 := mdAddr
	md2.Encoding = icatypes.EncodingProto3JSON
	v2 := string(icatypes.ModuleCdc.MustMarshalJSON(&md2))
	ch := channeltypes.NewChannel(channeltypes.OPEN, channeltypes.ORDERED, channeltypes.NewCounterparty(icatypes.HostPortID, hostChan), []string{ctrl.ConnectionID}, v1addr)
	ch.UpgradeSequence = 1
	sapp.IBCKeeper.ChannelKeeper.SetChannel(sim.GetContext(), ctrlPort, ctrlChan, ch)
	initUpgrade := channeltypes.NewUpgrade(channeltypes.UpgradeFields{Ordering: channeltypes.ORDERED, ConnectionHops: []string{ctrl.ConnectionID}, Version: v2}, channeltypes.Timeout{}, 0)
	sapp.IBCKeeper.ChannelKeeper.SetUpgrade(sim.GetContext(), ctrlPort, ctrlChan, initUpgrade)
	coordinator.CommitBlock(sim)

	hostEnd.ChannelConfig.ProposedUpgrade = channeltypes.Upgrade{Fields: channeltypes.UpgradeFields{Ordering: channeltypes.ORDERED, ConnectionHops: []string{hostEnd.ConnectionID}, Version: v2}}
	require.NoError(t, hostEnd.ChanUpgradeTry())
	require.Equal(t, channeltypes.FLUSHING, hostEnd.GetChannel().State)
	require.Equal(t, v1addr, hostEnd.GetChannel().Version, "the channel keeps proto3 until the upgrade completes")

	// 4. Counterparty ChanUpgradeAck (FLUSHCOMPLETE) and a packet commitment.
	payload := jsonSends(icaAddr, celestia.SenderAccount.GetAddress().String(), denom, nInner)
	msgsJSON, err := icatypes.DeserializeCosmosTx(capp.AppCodec(), payload, icatypes.EncodingProto3JSON)
	require.NoError(t, err)
	require.Len(t, msgsJSON, nInner)

	ch.State = channeltypes.FLUSHCOMPLETE
	sapp.IBCKeeper.ChannelKeeper.SetChannel(sim.GetContext(), ctrlPort, ctrlChan, ch)
	ackUpgrade := channeltypes.NewUpgrade(initUpgrade.Fields, channeltypes.NewTimeout(clienttypes.ZeroHeight(), uint64(coordinator.CurrentTime.Add(24*time.Hour).UnixNano())), 1)
	sapp.IBCKeeper.ChannelKeeper.SetUpgrade(sim.GetContext(), ctrlPort, ctrlChan, ackUpgrade)
	data := icatypes.InterchainAccountPacketData{Type: icatypes.EXECUTE_TX, Data: payload}
	packet := channeltypes.NewPacket(data.GetBytes(), 1, ctrlPort, ctrlChan, icatypes.HostPortID, hostChan,
		clienttypes.ZeroHeight(), uint64(coordinator.CurrentTime.Add(24*time.Hour).UnixNano()))
	sapp.IBCKeeper.ChannelKeeper.SetPacketCommitment(sim.GetContext(), ctrlPort, ctrlChan, 1, channeltypes.CommitPacket(sapp.AppCodec(), packet))
	coordinator.CommitBlock(sim)
	require.NoError(t, hostEnd.UpdateClient())

	chProof, upProof, proofHeight := ctrl.QueryChannelUpgradeProof()
	pktProof, pktHeight := sim.QueryProof(host.PacketCommitmentKey(ctrlPort, ctrlChan, 1))
	signer := celestia.SenderAccount.GetAddress().String()
	confirm := channeltypes.NewMsgChannelUpgradeConfirm(icatypes.HostPortID, hostChan, channeltypes.FLUSHCOMPLETE, ackUpgrade, chProof, upProof, proofHeight, signer)
	recv := channeltypes.NewMsgRecvPacket(packet, pktProof, pktHeight, signer)

	// 5. One Celestia tx: [ChanUpgradeConfirm, RecvPacket].
	coordinator.UpdateTimeForChain(celestia)
	tx, err := simtestutil.GenSignedMockTx(rand.New(rand.NewSource(1)), celestia.TxConfig, []sdk.Msg{confirm, recv},
		sdk.Coins{sdk.NewInt64Coin(denom, 400_000)}, 90_000_000, celestia.ChainID,
		[]uint64{celestia.SenderAccount.GetAccountNumber()}, []uint64{celestia.SenderAccount.GetSequence()}, celestia.SenderPrivKey)
	require.NoError(t, err)
	txBytes, err := celestia.TxConfig.TxEncoder()(tx)
	require.NoError(t, err)

	checkRes, err := capp.CheckTx(&abci.RequestCheckTx{Tx: txBytes, Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.Equal(t, apperr.ErrTxExceedsMaxSDKMessages.ABCICode(), checkRes.Code, "CheckTx log: %s", checkRes.Log)

	height := capp.LastBlockHeight() + 1
	blockTime := celestia.CurrentHeader.Time
	prep, err := capp.PrepareProposal(&abci.RequestPrepareProposal{Height: height, Time: blockTime, Txs: [][]byte{txBytes}, MaxTxBytes: 64 << 20})
	require.NoError(t, err)
	require.Empty(t, prep.Txs, "PrepareProposal must drop the tx")

	// A proposer that includes the tx anyway must have its block rejected.
	txs := [][]byte{txBytes}
	classified, err := fibretypes.ClassifyTxs(txs)
	require.NoError(t, err)
	dataSquare, err := square.Construct(classified, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
	require.NoError(t, err)
	size, err := dataSquare.Size()
	require.NoError(t, err)
	eds, err := da.ExtendShares(share.ToBytes(dataSquare))
	require.NoError(t, err)
	dah, err := da.NewDataAvailabilityHeader(eds)
	require.NoError(t, err)
	proc, err := capp.ProcessProposal(&abci.RequestProcessProposal{
		Height: height, Time: blockTime, Txs: txs,
		SquareSize: uint64(size), DataRootHash: dah.Hash(),
	})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_REJECT, proc.Status)
}
