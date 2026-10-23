package app_test

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	hooktypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/02_post_dispatch/types"
	coretypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	cmttypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"
)

// hyperlaneVerifyResult holds the consensus-relevant fields of a tx result.
type hyperlaneVerifyResult struct {
	scenario  string
	gasLimit  uint64
	code      uint32
	codespace string
	gasUsed   int64
}

// hyperlaneVerifyGasLimits are the tx gas limits each scenario runs with.
var hyperlaneVerifyGasLimits = []uint64{200_000, 1_000_003, 10_000_019, 123_456_789, 500_000_001}

// expectedHyperlaneVerifyResults were recorded with hyperlane-cosmos v1.3.0.
var expectedHyperlaneVerifyResults = []hyperlaneVerifyResult{
	{scenario: "leaf", gasLimit: 200000, code: 1, codespace: "undefined", gasUsed: 99403},
	{scenario: "leaf", gasLimit: 1000003, code: 1, codespace: "undefined", gasUsed: 86177},
	{scenario: "leaf", gasLimit: 10000019, code: 1, codespace: "undefined", gasUsed: 86187},
	{scenario: "leaf", gasLimit: 123456789, code: 1, codespace: "undefined", gasUsed: 86187},
	{scenario: "leaf", gasLimit: 500000001, code: 1, codespace: "undefined", gasUsed: 86197},
	{scenario: "routing -> leaf", gasLimit: 200000, code: 1, codespace: "undefined", gasUsed: 97936},
	{scenario: "routing -> leaf", gasLimit: 1000003, code: 1, codespace: "undefined", gasUsed: 97936},
	{scenario: "routing -> leaf", gasLimit: 10000019, code: 1, codespace: "undefined", gasUsed: 97946},
	{scenario: "routing -> leaf", gasLimit: 123456789, code: 1, codespace: "undefined", gasUsed: 97946},
	{scenario: "routing -> leaf", gasLimit: 500000001, code: 1, codespace: "undefined", gasUsed: 97986},
	{scenario: "routing -> routing -> leaf", gasLimit: 200000, code: 1, codespace: "undefined", gasUsed: 109728},
	{scenario: "routing -> routing -> leaf", gasLimit: 1000003, code: 1, codespace: "undefined", gasUsed: 109728},
	{scenario: "routing -> routing -> leaf", gasLimit: 10000019, code: 1, codespace: "undefined", gasUsed: 109738},
	{scenario: "routing -> routing -> leaf", gasLimit: 123456789, code: 1, codespace: "undefined", gasUsed: 109738},
	{scenario: "routing -> routing -> leaf", gasLimit: 500000001, code: 1, codespace: "undefined", gasUsed: 109748},
	{scenario: "self route", gasLimit: 200000, code: 11, codespace: "sdk", gasUsed: 200654},
	{scenario: "self route", gasLimit: 1000003, code: 11, codespace: "sdk", gasUsed: 1000266},
	{scenario: "self route", gasLimit: 10000019, code: 11, codespace: "sdk", gasUsed: 10007670},
	{scenario: "self route", gasLimit: 123456789, code: 11, codespace: "sdk", gasUsed: 123458502},
	{scenario: "self route", gasLimit: 500000001, code: 11, codespace: "sdk", gasUsed: 500005210},
	{scenario: "two hop cycle", gasLimit: 200000, code: 11, codespace: "sdk", gasUsed: 200654},
	{scenario: "two hop cycle", gasLimit: 1000003, code: 11, codespace: "sdk", gasUsed: 1000266},
	{scenario: "two hop cycle", gasLimit: 10000019, code: 11, codespace: "sdk", gasUsed: 10007670},
	{scenario: "two hop cycle", gasLimit: 123456789, code: 11, codespace: "sdk", gasUsed: 123458502},
	{scenario: "two hop cycle", gasLimit: 500000001, code: 11, codespace: "sdk", gasUsed: 500005210},
	{scenario: "three hop cycle", gasLimit: 200000, code: 11, codespace: "sdk", gasUsed: 200654},
	{scenario: "three hop cycle", gasLimit: 1000003, code: 11, codespace: "sdk", gasUsed: 1000266},
	{scenario: "three hop cycle", gasLimit: 10000019, code: 11, codespace: "sdk", gasUsed: 10007670},
	{scenario: "three hop cycle", gasLimit: 123456789, code: 11, codespace: "sdk", gasUsed: 123458502},
	{scenario: "three hop cycle", gasLimit: 500000001, code: 11, codespace: "sdk", gasUsed: 500005210},
	{scenario: "tail into cycle", gasLimit: 200000, code: 11, codespace: "sdk", gasUsed: 200654},
	{scenario: "tail into cycle", gasLimit: 1000003, code: 11, codespace: "sdk", gasUsed: 1000266},
	{scenario: "tail into cycle", gasLimit: 10000019, code: 11, codespace: "sdk", gasUsed: 10007670},
	{scenario: "tail into cycle", gasLimit: 123456789, code: 11, codespace: "sdk", gasUsed: 123458502},
	{scenario: "tail into cycle", gasLimit: 500000001, code: 11, codespace: "sdk", gasUsed: 500005210},
	{scenario: "missing route", gasLimit: 200000, code: 8, codespace: "ism", gasUsed: 84823},
	{scenario: "missing route", gasLimit: 1000003, code: 8, codespace: "ism", gasUsed: 84823},
	{scenario: "missing route", gasLimit: 10000019, code: 8, codespace: "ism", gasUsed: 84833},
	{scenario: "missing route", gasLimit: 123456789, code: 8, codespace: "ism", gasUsed: 84833},
	{scenario: "missing route", gasLimit: 500000001, code: 8, codespace: "ism", gasUsed: 84843},
}

// expectedHyperlaneVerifyResultsHash is the LastResultsHash of the block,
// recorded with hyperlane-cosmos v1.3.0.
const expectedHyperlaneVerifyResultsHash = "0b3f6001eb18d02dde490b185d2a7297cc54fa7f9569558a9f41b7825b75443b"

// TestHyperlaneVerifyConsensus executes MsgProcessMessage through
// FinalizeBlock for leaf, routing, cyclic and misconfigured ISMs at several
// gas limits. The tx results must match the results recorded with
// hyperlane-cosmos v1.3.0, so that an upgraded node computes the same
// LastResultsHash as a node that has not upgraded.
func TestHyperlaneVerifyConsensus(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(1)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	owner := testfactory.GetAddress(kr, accounts[0]).String()

	type scenario struct {
		name      string
		mailbox   util.HexAddress
		recipient util.HexAddress
		origin    uint32
	}
	var scenarios []scenario

	execBlock(t, testApp, func(ctx sdk.Context) {
		deliver := func(msg sdk.Msg, resp proto.Message) {
			res, err := testApp.MsgServiceRouter().Handler(msg)(ctx, msg)
			require.NoError(t, err)
			require.NoError(t, proto.Unmarshal(res.MsgResponses[0].Value, resp))
		}
		routingIsm := func() util.HexAddress {
			var resp ismtypes.MsgCreateRoutingIsmResponse
			deliver(&ismtypes.MsgCreateRoutingIsm{Creator: owner}, &resp)
			return resp.Id
		}
		setRoute := func(ism, to util.HexAddress) {
			deliver(&ismtypes.MsgSetRoutingIsmDomain{Owner: owner, IsmId: ism, Route: ismtypes.Route{Ism: to, Domain: 1}}, &ismtypes.MsgSetRoutingIsmDomainResponse{})
		}
		var hook hooktypes.MsgCreateNoopHookResponse
		deliver(&hooktypes.MsgCreateNoopHook{Owner: owner}, &hook)
		add := func(name string, ism util.HexAddress, origin uint32) {
			var mailbox coretypes.MsgCreateMailboxResponse
			deliver(&coretypes.MsgCreateMailbox{Owner: owner, LocalDomain: 1, DefaultIsm: ism, DefaultHook: &hook.Id, RequiredHook: &hook.Id}, &mailbox)
			var token warptypes.MsgCreateCollateralTokenResponse
			deliver(&warptypes.MsgCreateCollateralToken{Owner: owner, OriginMailbox: mailbox.Id, OriginDenom: appconsts.BondDenom}, &token)
			scenarios = append(scenarios, scenario{name: name, mailbox: mailbox.Id, recipient: token.Id, origin: origin})
		}

		var noop ismtypes.MsgCreateNoopIsmResponse
		deliver(&ismtypes.MsgCreateNoopIsm{Creator: owner}, &noop)
		direct := routingIsm()
		setRoute(direct, noop.Id)
		outer := routingIsm()
		setRoute(outer, direct)
		self := routingIsm()
		setRoute(self, self)
		a, b := routingIsm(), routingIsm()
		setRoute(a, b)
		setRoute(b, a)
		c1, c2, c3 := routingIsm(), routingIsm(), routingIsm()
		setRoute(c1, c2)
		setRoute(c2, c3)
		setRoute(c3, c1)
		tail := routingIsm()
		setRoute(tail, a)

		add("leaf", noop.Id, 1)
		add("routing -> leaf", direct, 1)
		add("routing -> routing -> leaf", outer, 1)
		add("self route", self, 1)
		add("two hop cycle", a, 1)
		add("three hop cycle", c1, 1)
		add("tail into cycle", tail, 1)
		add("missing route", direct, 2)
	})

	signer := newSignerFactory(t, kr, enc.TxConfig, accounts, queryAccountInfo(testApp, accounts, kr))(0)
	txs := make([][]byte, 0, len(scenarios)*len(hyperlaneVerifyGasLimits))
	got := make([]hyperlaneVerifyResult, 0, len(scenarios)*len(hyperlaneVerifyGasLimits))
	for _, sc := range scenarios {
		for _, gasLimit := range hyperlaneVerifyGasLimits {
			message := util.HyperlaneMessage{
				Version:     3,
				Nonce:       uint32(len(txs)),
				Origin:      sc.origin,
				Destination: 1,
				Recipient:   sc.recipient,
			}
			msg := &coretypes.MsgProcessMessage{
				MailboxId: sc.mailbox,
				Relayer:   owner,
				Metadata:  "0x",
				Message:   util.EncodeEthHex(message.Bytes()),
			}
			tx, _, err := signer.CreateTx([]sdk.Msg{msg}, user.SetGasLimit(gasLimit), user.SetFee(1_000_000))
			require.NoError(t, err)
			require.NoError(t, signer.IncrementSequence(accounts[0]))
			txs = append(txs, tx)
			got = append(got, hyperlaneVerifyResult{scenario: sc.name, gasLimit: gasLimit})
		}
	}

	resp := commitBlockAt(t, testApp, testutil.GenesisTime.Add(time.Hour), txs...)
	require.Len(t, resp.TxResults, len(txs))
	for i, result := range resp.TxResults {
		got[i].code = result.Code
		got[i].codespace = result.Codespace
		got[i].gasUsed = result.GasUsed
	}
	resultsHash := hex.EncodeToString(cmttypes.NewResults(resp.TxResults).Hash())

	require.Equal(t, expectedHyperlaneVerifyResults, got)
	require.Equal(t, expectedHyperlaneVerifyResultsHash, resultsHash)
}
