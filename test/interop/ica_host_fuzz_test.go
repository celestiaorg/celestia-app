package interop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/gogoproto/proto"
	icahosttypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host/types"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v8/modules/core/05-port/types"
	host "github.com/cosmos/ibc-go/v8/modules/core/24-host"
	ibcexported "github.com/cosmos/ibc-go/v8/modules/core/exported"
	ibctesting "github.com/cosmos/ibc-go/v8/testing"
	"github.com/stretchr/testify/require"
)

var icaFuzzEncodings = []string{icatypes.EncodingProtobuf, icatypes.EncodingProto3JSON}

// Stable, same-length placeholders keep seed bytes identical across fuzz workers.
// ibctesting gives each worker fresh keys and time-dependent ICA addresses.
func icaFuzzAccount(n byte) sdk.AccAddress { return sdk.AccAddress(bytes.Repeat([]byte{n}, 32)) }
func icaFuzzRelayer() sdk.AccAddress       { return sdk.AccAddress(bytes.Repeat([]byte{3}, 20)) }
func icaFuzzValidator() sdk.ValAddress     { return sdk.ValAddress(bytes.Repeat([]byte{4}, 20)) }

// FuzzICAHostOnRecvPacket executes arbitrary CosmosTx bytes on open protobuf
// and proto3json channels. Each input runs twice on independent caches of the
// same state. Acks, events, gas and all bank balances must agree; accepted
// messages must be allowlisted and signed only by that channel's ICA.
//
// These direct callbacks skip ibc-go core's proof checks, error-ack cache and
// ack commitment. The host's own atomic execution is exercised. This is not
// a sequence fuzzer or a comparison across architectures or Go toolchains.
func FuzzICAHostOnRecvPacket(f *testing.F) {
	for enc := range uint8(len(icaFuzzEncodings)) {
		for _, payload := range icaFuzzPayloads(f, enc) {
			f.Add(enc, int32(icatypes.EXECUTE_TX), payload, "")
		}
		f.Add(enc, int32(0), []byte(nil), "memo")
		f.Add(enc, int32(-1), []byte{0xff}, "")
		f.Add(enc, int32(icatypes.EXECUTE_TX), []byte{0x0a, 0xff}, "")
		f.Add(enc, int32(icatypes.EXECUTE_TX), []byte(`{"messages":[{"@type":"/unknown"}]}`), "")
	}
	var fx *icaHostFuzzFixture
	f.Fuzz(func(t *testing.T, enc uint8, packetType int32, payload []byte, memo string) {
		if len(payload)+len(memo) > 1<<20 {
			return
		}
		if fx == nil {
			fx = newICAHostFuzzFixture(t)
		}
		first := fx.recv(t, enc, packetType, payload, memo)
		second := fx.recv(t, enc, packetType, payload, memo)
		require.Equal(t, first, second, "same ICA packet on the same state gave different results")
	})
}

// FuzzICAHostOnChanOpenTry exercises untrusted controller ports, metadata and
// ordering. Invalid JSON may negotiate default metadata; successful handshakes
// must return valid metadata naming the account actually registered in state.
func FuzzICAHostOnChanOpenTry(f *testing.F) {
	for _, enc := range icaFuzzEncodings {
		md := icatypes.NewMetadata(icatypes.Version, "connection-0", "connection-0", "", enc, icatypes.TxTypeSDKMultiMsg)
		version := string(icatypes.ModuleCdc.MustMarshalJSON(&md))
		for _, order := range []channeltypes.Order{channeltypes.NONE, channeltypes.ORDERED, channeltypes.UNORDERED} {
			f.Add("icacontroller-fuzz", version, int32(order))
		}
		md.Address = icaFuzzAccount(1).String()
		f.Add("icacontroller-fuzz", string(icatypes.ModuleCdc.MustMarshalJSON(&md)), int32(channeltypes.ORDERED))
	}
	for _, version := range []string{"", "not json", "{}", "null", `{"version":"ics27-1"}`} {
		f.Add("icacontroller-fuzz", version, int32(channeltypes.ORDERED))
	}
	f.Add("icacontroller-fuzz-0", "", int32(channeltypes.ORDERED)) // already active
	f.Add("", "", int32(-1))
	var fx *icaHostFuzzFixture
	f.Fuzz(func(t *testing.T, port, version string, order int32) {
		if len(port)+len(version) > 1<<20 {
			return
		}
		if fx == nil {
			fx = newICAHostFuzzFixture(t)
		}
		fx.handshake(t, port, version, order)
	})
}

// Include successful sends/delegations, a foreign signer, a forbidden message,
// and an execution failure after a valid send (which must roll back that send).
func icaFuzzPayloads(t testing.TB, enc uint8) [][]byte {
	t.Helper()
	cdc := encoding.MakeConfig(app.ModuleEncodingRegisters...).Codec
	account := icaFuzzAccount(enc + 1)
	coins := sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 10))
	send := banktypes.NewMsgSend(account, icaFuzzRelayer(), coins)
	delegate := &stakingtypes.MsgDelegate{DelegatorAddress: account.String(), ValidatorAddress: icaFuzzValidator().String(), Amount: coins[0]}
	fail := banktypes.NewMsgSend(account, icaFuzzRelayer(), sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 1_000_001)))
	foreign := banktypes.NewMsgSend(icaFuzzRelayer(), account, coins)
	forbidden := &banktypes.MsgMultiSend{
		Inputs:  []banktypes.Input{{Address: account.String(), Coins: coins}},
		Outputs: []banktypes.Output{{Address: icaFuzzRelayer().String(), Coins: coins}},
	}
	seeds := [][]proto.Message{{send}, {delegate}, {send, delegate}, {foreign}, {forbidden}, {send, fail}, {send, foreign}, {send, forbidden}, {}}
	payloads := make([][]byte, 0, len(seeds))
	for _, msgs := range seeds {
		payload, err := icatypes.SerializeCosmosTx(cdc, msgs, icaFuzzEncodings[enc])
		require.NoError(t, err)
		payloads = append(payloads, payload)
	}
	return payloads
}

type icaFuzzChannel struct {
	host, controller *ibctesting.Endpoint
	account          sdk.AccAddress
}

type icaHostFuzzFixture struct {
	app       *app.App
	ctx       sdk.Context
	stack     porttypes.IBCModule
	channels  []icaFuzzChannel
	relayer   sdk.AccAddress
	validator string
	balances  map[string]sdk.Coins
}

func newICAHostFuzzFixture(t *testing.T) *icaHostFuzzFixture {
	t.Helper()
	coordinator, celestia, sim, _ := SetupTest(t)
	capp := celestia.App.(*app.App)
	sapp := sim.App.(*SimApp)
	stack, ok := capp.IBCKeeper.Router.GetRoute(icahosttypes.SubModuleName)
	require.True(t, ok)
	fx := &icaHostFuzzFixture{app: capp, stack: stack, relayer: celestia.SenderAccount.GetAddress()}
	for i, enc := range icaFuzzEncodings {
		path := ibctesting.NewPath(celestia, sim)
		coordinator.SetupConnections(path)
		hostEnd, ctrl := path.EndpointA, path.EndpointB
		ctrlPort, ctrlChan := fmt.Sprintf("icacontroller-fuzz-%d", i), fmt.Sprintf("channel-%d", 100+i)
		ctrl.ChannelConfig.PortID, ctrl.ChannelID = ctrlPort, ctrlChan
		ctrl.ChannelConfig.Order = channeltypes.ORDERED
		hostEnd.ChannelConfig.PortID = icatypes.HostPortID
		hostEnd.ChannelConfig.Order = channeltypes.ORDERED
		md := icatypes.NewMetadata(icatypes.Version, ctrl.ConnectionID, hostEnd.ConnectionID, "", enc, icatypes.TxTypeSDKMultiMsg)
		version := string(icatypes.ModuleCdc.MustMarshalJSON(&md))
		ctrl.ChannelConfig.Version, hostEnd.ChannelConfig.Version = version, version
		// Write the controller side directly, as in TestICAPacketCountIsEncodingIndependent.
		sapp.IBCKeeper.ChannelKeeper.SetChannel(sim.GetContext(), ctrlPort, ctrlChan, channeltypes.NewChannel(
			channeltypes.INIT, channeltypes.ORDERED, channeltypes.NewCounterparty(icatypes.HostPortID, ""), []string{ctrl.ConnectionID}, version))
		coordinator.CommitBlock(sim)
		require.NoError(t, hostEnd.ChanOpenTry())
		version = hostEnd.GetChannel().Version
		sapp.IBCKeeper.ChannelKeeper.SetChannel(sim.GetContext(), ctrlPort, ctrlChan, channeltypes.NewChannel(
			channeltypes.OPEN, channeltypes.ORDERED, channeltypes.NewCounterparty(icatypes.HostPortID, hostEnd.ChannelID), []string{ctrl.ConnectionID}, version))
		coordinator.CommitBlock(sim)
		require.NoError(t, hostEnd.ChanOpenConfirm())
		require.Equal(t, channeltypes.OPEN, hostEnd.GetChannel().State)
		md, err := icatypes.MetadataFromVersion(version)
		require.NoError(t, err)
		account, err := sdk.AccAddressFromBech32(md.Address)
		require.NoError(t, err)
		require.NoError(t, capp.BankKeeper.SendCoins(celestia.GetContext(), fx.relayer, account, sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 1_000_000))))
		fx.channels = append(fx.channels, icaFuzzChannel{hostEnd, ctrl, account})
	}
	fx.ctx = celestia.GetContext()
	validators, err := capp.StakingKeeper.GetAllValidators(fx.ctx)
	require.NoError(t, err)
	require.NotEmpty(t, validators)
	fx.validator = validators[0].OperatorAddress
	fx.balances = fx.allBalances(fx.ctx)
	return fx
}

func (fx *icaHostFuzzFixture) replaceAddresses(t *testing.T, payload []byte) []byte {
	t.Helper()
	replace := func(from, to string) {
		require.Len(t, to, len(from), "placeholder replacement must preserve protobuf lengths")
		payload = bytes.ReplaceAll(payload, []byte(from), []byte(to))
	}
	for i, ch := range fx.channels {
		replace(icaFuzzAccount(byte(i+1)).String(), ch.account.String())
	}
	replace(icaFuzzRelayer().String(), fx.relayer.String())
	replace(icaFuzzValidator().String(), fx.validator)
	return payload
}

func (fx *icaHostFuzzFixture) allBalances(ctx sdk.Context) map[string]sdk.Coins {
	balances := make(map[string]sdk.Coins)
	fx.app.BankKeeper.IterateAllBalances(ctx, func(address sdk.AccAddress, coin sdk.Coin) bool {
		if !coin.IsZero() {
			balances[address.String()] = balances[address.String()].Add(coin)
		}
		return false
	})
	return balances
}

type icaFuzzResult struct {
	Ack      []byte
	Success  bool
	Events   []abci.Event
	Gas      uint64
	Balances map[string]sdk.Coins
}

func (fx *icaHostFuzzFixture) recv(t *testing.T, enc uint8, packetType int32, payload []byte, memo string) icaFuzzResult {
	t.Helper()
	index := int(enc) % len(fx.channels)
	ch := fx.channels[index]
	payload = fx.replaceAddresses(t, payload)
	// Use encoding/json so even unknown numeric enum values reach the host.
	data, err := json.Marshal(struct {
		Type int32  `json:"type"`
		Data []byte `json:"data"`
		Memo string `json:"memo"`
	}{packetType, payload, memo})
	require.NoError(t, err)
	packet := channeltypes.NewPacket(data, 1, ch.controller.ChannelConfig.PortID, ch.controller.ChannelID,
		icatypes.HostPortID, ch.host.ChannelID, clienttypes.NewHeight(1, 1000), 0)
	ctx, _ := fx.ctx.CacheContext()
	ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
	var ack ibcexported.Acknowledgement
	mustNotPanic(t, "ICA OnRecvPacket", func() { ack = fx.stack.OnRecvPacket(ctx, packet, fx.relayer) })
	require.NotNil(t, ack)
	a, ok := ack.(channeltypes.Acknowledgement)
	require.True(t, ok, "ack type %T", ack)
	// Capture gas before oracle reads; they are not part of callback execution.
	res := icaFuzzResult{Ack: a.Acknowledgement(), Success: a.Success(), Events: ctx.EventManager().ABCIEvents(), Gas: ctx.GasMeter().GasConsumed(), Balances: fx.allBalances(ctx)}
	if a.Success() {
		require.IsType(t, &channeltypes.Acknowledgement_Result{}, a.Response)
		require.Equal(t, int32(icatypes.EXECUTE_TX), packetType)
		msgs, err := icatypes.DeserializeCosmosTx(fx.app.AppCodec(), payload, icaFuzzEncodings[index])
		require.NoError(t, err, "accepted payload must decode")
		allow := fx.app.ICAHostKeeper.GetParams(fx.ctx).AllowMessages
		for _, msg := range msgs {
			require.Contains(t, allow, sdk.MsgTypeURL(msg), "accepted message not allowlisted")
			signers, _, err := fx.app.AppCodec().GetMsgV1Signers(msg)
			require.NoError(t, err)
			for _, signer := range signers {
				require.Equal(t, ch.account, sdk.AccAddress(signer), "foreign signer accepted")
			}
		}
		for addr, before := range fx.balances {
			if addr == ch.account.String() {
				continue
			}
			require.True(t, res.Balances[addr].IsAllGTE(before), "non-ICA account %s lost funds", addr)
		}
	} else {
		require.IsType(t, &channeltypes.Acknowledgement_Error{}, a.Response)
		require.Regexp(t, codeOnlyErrorAck, a.GetError())
		require.Equal(t, fx.balances, res.Balances, "rejected packet changed balances")
	}
	return res
}

func (fx *icaHostFuzzFixture) handshake(t *testing.T, port, version string, order int32) bool {
	t.Helper()
	ctx, _ := fx.ctx.CacheContext()
	ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
	const channelID = "channel-999"
	ch := fx.channels[0]
	hops := []string{ch.host.ConnectionID}
	cap, err := fx.app.ScopedIBCKeeper.NewCapability(ctx, host.ChannelCapabilityPath(icatypes.HostPortID, channelID))
	require.NoError(t, err)
	activeID, active := fx.app.ICAHostKeeper.GetActiveChannelID(ctx, ch.host.ConnectionID, port)
	if active {
		existing, found := fx.app.IBCKeeper.ChannelKeeper.GetChannel(ctx, icatypes.HostPortID, activeID)
		active = !found || existing.State != channeltypes.CLOSED
	}
	var negotiated string
	mustNotPanic(t, "ICA OnChanOpenTry", func() {
		negotiated, err = fx.stack.OnChanOpenTry(ctx, channeltypes.Order(order), hops, icatypes.HostPortID, channelID, cap,
			channeltypes.NewCounterparty(port, "channel-999"), version)
	})
	if active {
		require.Error(t, err, "reopened active channel %s for port %q", activeID, port)
	}
	if err != nil {
		return false
	}
	md, err := icatypes.MetadataFromVersion(negotiated)
	require.NoError(t, err)
	require.NoError(t, icatypes.ValidateHostMetadata(ctx, fx.app.IBCKeeper.ChannelKeeper, hops, md))
	require.Equal(t, icatypes.Version, md.Version)
	require.Equal(t, ch.host.ConnectionID, md.HostConnectionId)
	require.Equal(t, ch.controller.ConnectionID, md.ControllerConnectionId)
	require.Contains(t, icaFuzzEncodings, md.Encoding)
	require.Equal(t, icatypes.TxTypeSDKMultiMsg, md.TxType)
	address, found := fx.app.ICAHostKeeper.GetInterchainAccountAddress(ctx, ch.host.ConnectionID, port)
	require.True(t, found)
	require.Equal(t, address, md.Address)
	account, err := sdk.AccAddressFromBech32(address)
	require.NoError(t, err)
	registered, ok := fx.app.AccountKeeper.GetAccount(ctx, account).(*icatypes.InterchainAccount)
	require.True(t, ok, "registered account must be an ICA")
	require.Equal(t, port, registered.AccountOwner)
	return true
}

// Pin success and rejection paths so the targets cannot pass by rejecting
// every input, and verify placeholder seeds work against fresh fixture keys.
func TestICAHostFuzzFixtureAcceptsValidInputs(t *testing.T) {
	fx := newICAHostFuzzFixture(t)
	for enc := range uint8(len(icaFuzzEncodings)) {
		for i, payload := range icaFuzzPayloads(t, enc) {
			res := fx.recv(t, enc, int32(icatypes.EXECUTE_TX), payload, "memo")
			require.Equal(t, i < 3 || i == 8, res.Success, "encoding %s seed %d", icaFuzzEncodings[enc], i)
			require.Equal(t, res, fx.recv(t, enc, int32(icatypes.EXECUTE_TX), payload, "memo"))
			if i == 0 {
				addr := fx.channels[enc].account.String()
				require.Equal(t, fx.balances[addr].AmountOf(sdk.DefaultBondDenom).SubRaw(10), res.Balances[addr].AmountOf(sdk.DefaultBondDenom))
			}
		}
	}
	require.True(t, fx.handshake(t, "icacontroller-new", "", int32(channeltypes.ORDERED)))
	require.True(t, fx.handshake(t, "icacontroller-new", "", int32(channeltypes.ORDERED)), "handshake cache must not leak")
	require.False(t, fx.handshake(t, "icacontroller-fuzz-0", "", int32(channeltypes.ORDERED)), "active ICA must not be reopened")
}
