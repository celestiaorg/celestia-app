package interop

import (
	"encoding/json"
	"regexp"
	"runtime/debug"
	"strings"
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v11/app"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward"
	pfmtypes "github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward/types"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v8/modules/core/05-port/types"
	ibcexported "github.com/cosmos/ibc-go/v8/modules/core/exported"
	ibctesting "github.com/cosmos/ibc-go/v8/testing"
	"github.com/stretchr/testify/require"
)

// ibctesting assigns channel IDs in order, and the seeds name these channels:
// chainA's channel to celestia, which prefixes a returning denom, and
// celestia's channel to chainB, which forward memos name.
const (
	transferFuzzSourceChannel  = "channel-0"
	transferFuzzForwardChannel = "channel-1"
)

// What happens to the forwarded packet, picked by transferFuzzInput.forwardOutcome.
const (
	forwardAcked          = iota // chainB acks it with success
	forwardAckedWithError        // chainB acks it with an error carrying the fuzzed text
	forwardAckedRaw              // the fuzzed bytes arrive as the ack, possibly malformed
	forwardTimedOut              // it times out: PFM retries or refunds
	numForwardOutcomes           // count of the outcomes above
)

// codeOnlyErrorAck is the error acknowledgement text that
// channeltypes.NewErrorAcknowledgement commits: it keeps just the ABCI code.
var codeOnlyErrorAck = regexp.MustCompile(`^ABCI code: \d+: error handling packet: see events for details$`)

var transferFixture *transferFuzzFixture

// FuzzTransferStackOnRecvPacket sends packets from chainA with arbitrary
// memo, denom and amount through celestia's transfer stack
// (packet-forward-middleware, transfer) by calling its callbacks directly.
// When celestia forwards the packet, the forwarded packet is then acked or
// timed out. No callback may panic, the acks celestia commits must carry no
// error text, and two runs of the same input on the same state must emit,
// write and store the same things.
//
// Calling the callbacks skips ibc-go core: proofs, the cache that core
// discards on an error ack, and the ack commitment. Both runs happen in one
// process, so the comparison catches map order, pointers and scheduling, but
// not results that differ between CPU architectures or Go toolchains.
func FuzzTransferStackOnRecvPacket(f *testing.F) {
	forward := func(fields string) string {
		return `{"forward":{"receiver":"cosmos1receiver","port":"transfer","channel":"` + transferFuzzForwardChannel + `"` + fields + `}}`
	}
	memos := []string{
		"",
		"plain text",
		`{"wasm":{"contract":"x"}}`,
		forward(""),
		forward(`,"retries":1,"timeout":"10m"`),
		forward(`,"next":` + forward("")),
		forward(`,"next":"{\"forward\":{\"receiver\":\"r\",\"port\":\"transfer\",\"channel\":\"channel-0\"}}"`),
		`{"forward":{"receiver":"cosmos1receiver","port":"transfer","channel":"channel-9"}}`,
	}
	for _, memo := range memos {
		for outcome := range uint8(numForwardOutcomes) {
			f.Add(memo, "stake", "100", outcome, []byte("downstream error"))
		}
	}
	f.Add(forward(""), "transfer/"+transferFuzzSourceChannel+"/utia", "1", uint8(forwardAcked), []byte(nil))

	f.Fuzz(func(t *testing.T, memo, denom, amount string, forwardOutcome uint8, forwardAck []byte) {
		if len(memo)+len(denom)+len(amount)+len(forwardAck) > 1<<20 {
			return
		}
		if transferFixture == nil {
			transferFixture = newTransferFuzzFixture(t)
		}
		in := transferFuzzInput{memo: memo, denom: denom, amount: amount, forwardOutcome: forwardOutcome, forwardAck: forwardAck}
		first := transferFixture.run(t, in)
		second := transferFixture.run(t, in)
		require.Equal(t, first, second, "the same packet on the same state gave different results")
	})
}

// transferFuzzFixture holds an IBC topology of chainA -> celestia -> chainB,
// with a transfer channel on each hop. src and dst are the chainA and
// celestia ends of the first channel. Every run works on a cache of ctx, so
// the fixture never changes and is shared by all inputs.
type transferFuzzFixture struct {
	app      *app.App
	ctx      sdk.Context
	stack    porttypes.IBCModule
	src, dst *ibctesting.Endpoint
	relayer  sdk.AccAddress
	receiver string
}

func newTransferFuzzFixture(t *testing.T) *transferFuzzFixture {
	coordinator, celestia, chainA, chainB := SetupTest(t)
	pathA := ibctesting.NewTransferPath(chainA, celestia)
	pathB := ibctesting.NewTransferPath(celestia, chainB)
	coordinator.Setup(pathA)
	coordinator.Setup(pathB)
	require.Equal(t, transferFuzzSourceChannel, pathA.EndpointA.ChannelID)
	require.Equal(t, transferFuzzForwardChannel, pathB.EndpointA.ChannelID)

	celestiaApp, ok := celestia.App.(*app.App)
	require.True(t, ok)
	stack, ok := celestiaApp.IBCKeeper.Router.GetRoute(transfertypes.ModuleName)
	require.True(t, ok)
	ctx := celestia.GetContext()
	// run checks the in-flight records a forward leaves, so start with none.
	require.Empty(t, celestiaApp.PacketForwardKeeper.ExportGenesis(ctx).InFlightPackets)

	return &transferFuzzFixture{
		app:      celestiaApp,
		ctx:      ctx,
		stack:    stack,
		src:      pathA.EndpointA,
		dst:      pathA.EndpointB,
		relayer:  celestia.SenderAccount.GetAddress(),
		receiver: celestia.SenderAccount.GetAddress().String(),
	}
}

// transferFuzzInput is one packet from chainA and, when celestia forwards
// it, what happens to the forwarded packet.
type transferFuzzInput struct {
	memo, denom, amount string
	// forwardOutcome picks how the forwarded packet ends (see forwardAcked
	// and the constants after it). forwardAck is the fuzzed ack text or bytes.
	forwardOutcome uint8
	forwardAck     []byte
}

func (in transferFuzzInput) outcome() uint8 { return in.forwardOutcome % numForwardOutcomes }

// packetData encodes the input the way ibc-go does: JSON with sorted keys.
func (in transferFuzzInput) packetData(sender, receiver string) []byte {
	bz, _ := json.Marshal(struct {
		Amount   string `json:"amount"`
		Denom    string `json:"denom"`
		Memo     string `json:"memo,omitempty"`
		Receiver string `json:"receiver"`
		Sender   string `json:"sender"`
	}{in.amount, in.denom, in.memo, receiver, sender})
	return bz
}

// transferFuzzResult is everything a run committed or emitted. Two runs of
// the same input on the same state must produce equal results.
type transferFuzzResult struct {
	RecvAck    []byte
	ForwardErr bool
	WrittenAck []byte
	Events     []abci.Event
	InFlight   map[string]pfmtypes.InFlightPacket
	GasUsed    uint64
}

func (fx *transferFuzzFixture) run(t *testing.T, in transferFuzzInput) transferFuzzResult {
	t.Helper()
	ctx, _ := fx.ctx.CacheContext()
	ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
	packet := channeltypes.NewPacket(
		in.packetData(fx.src.Chain.SenderAccount.GetAddress().String(), fx.receiver), 1,
		fx.src.ChannelConfig.PortID, fx.src.ChannelID, fx.dst.ChannelConfig.PortID, fx.dst.ChannelID,
		clienttypes.NewHeight(1, 1000), 0,
	)

	var res transferFuzzResult
	var ack ibcexported.Acknowledgement
	mustNotPanic(t, "OnRecvPacket", func() { ack = fx.stack.OnRecvPacket(ctx, packet, fx.relayer) })
	if ack != nil {
		res.RecvAck = ack.Acknowledgement()
		requireDeterministicAck(t, ack)
	} else {
		// A nil ack means packet-forward-middleware sent the packet on and
		// writes the ack once the forwarded packet is acked or times out.
		fwd, err := ibctesting.ParsePacketFromEvents(ctx.EventManager().ABCIEvents())
		require.NoError(t, err, "async ack without a forwarded packet")
		err = fx.finishForward(t, ctx, fwd, in)
		// Only fuzzed raw ack bytes may be rejected. A valid ack or a timeout
		// must complete the forward.
		if in.outcome() != forwardAckedRaw {
			require.NoError(t, err, "forward outcome %d", in.outcome())
		}
		res.ForwardErr = err != nil
		if written, parseErr := ibctesting.ParseAckFromEvents(ctx.EventManager().ABCIEvents()); err == nil && parseErr == nil {
			res.WrittenAck = written
		}
	}
	res.Events = ctx.EventManager().ABCIEvents()
	res.InFlight = fx.app.PacketForwardKeeper.ExportGenesis(ctx).InFlightPackets
	res.GasUsed = ctx.GasMeter().GasConsumed()
	if ack == nil && !res.ForwardErr {
		requireForwardDone(t, in, res)
	}
	return res
}

// requireForwardDone checks that a forward ended once the forwarded packet
// was acked or timed out: PFM wrote the ack of the original packet and removed
// its in-flight record, or, on a timeout with retries left, sent the packet
// again under a new in-flight record and wrote no ack yet.
func requireForwardDone(t *testing.T, in transferFuzzInput, res transferFuzzResult) {
	t.Helper()
	if in.outcome() == forwardTimedOut && res.WrittenAck == nil {
		require.Len(t, res.InFlight, 1, "a retried timeout must leave one in-flight record")
		return
	}
	require.NotNil(t, res.WrittenAck, "no ack written for the original packet")
	require.Empty(t, res.InFlight, "in-flight record left after the forward ended")
	if in.outcome() == forwardTimedOut {
		// With no retries left, PFM writes its own error ack for the
		// original packet.
		var a channeltypes.Acknowledgement
		require.NoError(t, channeltypes.SubModuleCdc.UnmarshalJSON(res.WrittenAck, &a))
		requireDeterministicAck(t, a)
	}
}

func (fx *transferFuzzFixture) finishForward(t *testing.T, ctx sdk.Context, fwd channeltypes.Packet, in transferFuzzInput) (err error) {
	t.Helper()
	var ackBz []byte
	switch in.outcome() {
	case forwardAcked:
		ackBz = channeltypes.NewResultAcknowledgement([]byte{1}).Acknowledgement()
	case forwardAckedWithError:
		ackBz = channeltypes.Acknowledgement{
			Response: &channeltypes.Acknowledgement_Error{Error: strings.ToValidUTF8(string(in.forwardAck), "")},
		}.Acknowledgement()
	case forwardAckedRaw:
		ackBz = in.forwardAck
	case forwardTimedOut:
		mustNotPanic(t, "OnTimeoutPacket", func() { err = fx.stack.OnTimeoutPacket(ctx, fwd, fx.relayer) })
		return err
	}
	mustNotPanic(t, "OnAcknowledgementPacket", func() { err = fx.stack.OnAcknowledgementPacket(ctx, fwd, ackBz, fx.relayer) })
	return err
}

// requireDeterministicAck fails unless ack is transfer's success ack or an
// error ack that carries no error text.
func requireDeterministicAck(t *testing.T, ack ibcexported.Acknowledgement) {
	t.Helper()
	a, ok := ack.(channeltypes.Acknowledgement)
	require.True(t, ok, "ack type %T", ack)
	if a.Success() {
		require.Equal(t, channeltypes.NewResultAcknowledgement([]byte{1}).Acknowledgement(), a.Acknowledgement())
		return
	}
	if a.GetError() == packetforward.ErrorAcknowledgementMessage {
		return
	}
	require.Regexp(t, codeOnlyErrorAck, a.GetError())
}

func mustNotPanic(t *testing.T, what string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s panicked: %v\n%s", what, r, debug.Stack())
		}
	}()
	fn()
}
