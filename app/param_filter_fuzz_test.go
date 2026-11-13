package app

import (
	"bytes"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app/ante"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/app/params"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/test/util/fuzzutil"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	coretypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

// paramFilterNodeWidth is the fixed width of one message-tree node record.
const paramFilterNodeWidth = 4

// paramTree builds a message tree from fuzz records and reports, by an
// independent reading of the governance param rules, whether the tree holds a
// param change that must be refused.
type paramTree struct {
	c     *fuzzutil.Consumer
	addr  sdk.AccAddress
	nodes int
}

// node returns the next message and whether ParamFilterDecorator must reject it
// when it appears inside a proposal or authz exec.
func (p *paramTree) node(depth int) (sdk.Msg, bool) {
	p.nodes++
	r := p.c.Record(paramFilterNodeWidth)
	kind, a, b, children := r.Byte(), r.Byte(), r.Byte(), int(r.Byte()%4)
	if depth >= 4 || p.nodes > 32 {
		kind %= 5 // leaves only
	}
	switch kind % 7 {
	case 0:
		return banktypes.NewMsgSend(p.addr, p.addr, sdk.NewCoins(sdk.NewInt64Coin(params.BondDenom, 1))), false
	case 1:
		msg := &banktypes.MsgUpdateParams{Authority: p.addr.String(), Params: banktypes.DefaultParams()}
		msg.Params.DefaultSendEnabled = a&1 == 0
		if a&2 != 0 {
			msg.Params.SendEnabled = []*banktypes.SendEnabled{{Denom: params.BondDenom, Enabled: b&1 == 0}} //nolint:staticcheck
		}
		return msg, a&1 != 0 || a&2 != 0
	case 2:
		msg := &stakingtypes.MsgUpdateParams{Authority: p.addr.String(), Params: stakingtypes.DefaultParams()}
		msg.Params.BondDenom = []string{params.BondDenom, "stake", "", params.BondDenom + " "}[a%4]
		msg.Params.UnbondingTime = appconsts.UnbondingTime + time.Duration(int8(b))*time.Second
		return msg, msg.Params.BondDenom != params.BondDenom || msg.Params.UnbondingTime != appconsts.UnbondingTime
	case 3, 4:
		defaults := coretypes.DefaultConsensusParams().ToProto()
		evidence := *EvidenceParams()
		validator := cmtproto.ValidatorParams{PubKeyTypes: append([]string(nil), defaults.Validator.PubKeyTypes...)}
		reject := false
		switch a % 4 {
		case 1:
			validator.PubKeyTypes = append(validator.PubKeyTypes, coretypes.ABCIPubKeyTypeSecp256k1)
			reject = true
		case 2:
			validator.PubKeyTypes = nil
			reject = true
		}
		switch b % 4 {
		case 1:
			evidence.MaxAgeNumBlocks++
			reject = true
		case 2:
			evidence.MaxAgeDuration -= time.Second
			reject = true
		case 3:
			evidence.MaxBytes = 0
			reject = true
		}
		msg := &consensustypes.MsgUpdateParams{
			Authority: p.addr.String(),
			Block:     defaults.Block,
			Evidence:  &evidence,
			Validator: &validator,
		}
		// ToProtoConsensusParams fails when any of these is missing.
		switch (a >> 2) % 5 {
		case 1:
			msg.Block = nil
			reject = true
		case 2:
			msg.Evidence = nil
			reject = true
		case 3:
			msg.Validator = nil
			reject = true
		}
		return msg, reject
	default: // 5: authz exec, 6: gov proposal
		var msgs []sdk.Msg
		reject := children == 0 // a wrapper must carry at least one message
		for range children {
			msg, r := p.node(depth + 1)
			msgs = append(msgs, msg)
			reject = reject || r
		}
		if kind%7 == 5 {
			exec := authz.NewMsgExec(p.addr, msgs)
			return &exec, reject
		}
		proposal, err := govv1.NewMsgSubmitProposal(msgs, sdk.NewCoins(), p.addr.String(), "", "t", "s", false)
		if err != nil {
			panic(err)
		}
		return proposal, reject
	}
}

// FuzzParamFilterDecorator builds transactions whose messages nest param
// changes for bank, staking and consensus inside governance proposals and authz
// execs, up to four levels deep, and runs them through the real tx encoder and
// decoder before ParamFilterDecorator with the app's governance filters. The
// decorator must reject a tx exactly when a wrapper holds a refused param
// change or is empty, at any depth, and only then; param messages outside a
// wrapper are left to other checks.
func FuzzParamFilterDecorator(f *testing.F) {
	node := func(kind, a, b, children byte) []byte { return []byte{kind, a, b, children} }
	f.Add([]byte{})
	f.Add(fuzzutil.Records(paramFilterNodeWidth, node(6, 0, 0, 1), node(1, 0, 0, 0)))
	f.Add(fuzzutil.Records(paramFilterNodeWidth, node(6, 0, 0, 1), node(1, 1, 0, 0)))
	f.Add(fuzzutil.Records(paramFilterNodeWidth, node(5, 0, 0, 2), node(0, 0, 0, 0), node(6, 0, 0, 1), node(2, 1, 0, 0)))
	f.Add(fuzzutil.Records(paramFilterNodeWidth, node(5, 0, 0, 1), node(5, 0, 0, 1), node(6, 0, 0, 1), node(3, 0, 1, 0)))
	f.Add(fuzzutil.Records(paramFilterNodeWidth, node(6, 0, 0, 1), node(3, 0, 0, 0)))
	f.Add(fuzzutil.Records(paramFilterNodeWidth, node(6, 0, 0, 1), node(4, 4, 0, 0)))
	f.Add(fuzzutil.Records(paramFilterNodeWidth, node(5, 0, 0, 0)))
	f.Add(fuzzutil.Records(paramFilterNodeWidth, node(1, 1, 0, 0), node(2, 1, 0, 0)))

	enc := encoding.MakeConfig(ModuleEncodingRegisters...)
	decorator := ante.NewParamFilterDecorator((*App)(nil).GovParamFilters())
	addr := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	f.Fuzz(func(t *testing.T, records []byte) {
		tree := &paramTree{c: fuzzutil.New(records), addr: addr}
		var msgs []sdk.Msg
		wantReject := false
		for len(msgs) < 4 && !tree.c.Exhausted() {
			msg, reject := tree.node(0)
			msgs = append(msgs, msg)
			switch msg.(type) {
			case *authz.MsgExec, *govv1.MsgSubmitProposal:
				wantReject = wantReject || reject
			}
		}
		if len(msgs) == 0 {
			msgs = append(msgs, banktypes.NewMsgSend(addr, addr, nil))
		}

		builder := enc.TxConfig.NewTxBuilder()
		require.NoError(t, builder.SetMsgs(msgs...))
		raw, err := enc.TxConfig.TxEncoder()(builder.GetTx())
		require.NoError(t, err)
		tx, err := enc.TxConfig.TxDecoder()(raw)
		require.NoError(t, err)

		nextCalled := false
		next := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
			nextCalled = true
			return ctx, nil
		}
		_, err = decorator.AnteHandle(sdk.Context{}, tx, false, next)
		if wantReject {
			require.Error(t, err, "a refused param change got past the filter")
			require.False(t, nextCalled)
		} else {
			require.NoError(t, err)
			require.True(t, nextCalled)
		}
	})
}
